// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The MCP transport carries the orchestrator family over the server's
// quicbaas_orchestrator_*, quicbaas_fleet_* and quicbaas_sdlc_worker_image*
// tools (pkg/api/mcp). Each verb is driven against a scripted Streamable HTTP
// server: the arguments it sends, the result it parses, and the McpError a
// tool failure becomes. Verbs the server has no tool for, and arguments a tool
// does not read, are refused with TransportNotSupportedError.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { McpClient } from '../src/mcp';
import { McpError, TransportNotSupportedError } from '../src/errors';
import type { PageResult } from '../src/page';
import { jsonReply, scriptServer, toolCalls, toolText } from './mcp_server';

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

// A wired verb: the call, the tool it must reach with which arguments, what
// the tool answers, and what the verb must return for that answer.
interface WiredVerb {
  name: string;
  call: (o: Orchestrator) => Promise<unknown>;
  tool: string;
  args: Record<string, unknown>;
  reply: unknown;
  want: unknown;
}

const node = { id: 'node-1', name: 'gpu-1', host: 'gpu-1.example', status: 'online' };
const policy = { tenant_id: 't1', limits: { windows: [{ name: 'day', size: 86400000000000, max_tokens: 10 }] } };
const costSettings = { settings: { tenant_id: 't1', electricity_price_per_kwh: 0.2 }, electricity: { price_per_kwh: 0.2, source: 'tenant' } };
const workerImage = { serving: true, running: false, queued: false };
const attentionItem = { task_id: 'task-1', kind: 'parked_task', subject: 's' };

const wired: WiredVerb[] = [
  {
    name: 'listJobs',
    call: (o) => o.listJobs('t1', 'task-1'),
    tool: 'quicbaas_orchestrator_list_jobs',
    args: { tenant_id: 't1', task_id: 'task-1' },
    reply: { jobs: [{ job_id: 'job-1', state: 'running' }], count: 1, total: 1 },
    want: [{ job_id: 'job-1', state: 'running' }],
  },
  {
    name: 'getJobStatus',
    call: (o) => o.getJobStatus('t1', 'job-1'),
    tool: 'quicbaas_orchestrator_get_job_status',
    args: { tenant_id: 't1', job_id: 'job-1' },
    reply: { job_id: 'job-1', state: 'done' },
    want: { job_id: 'job-1', state: 'done' },
  },
  {
    name: 'startJob',
    call: (o) => o.startJob({
      tenant_id: 't1', description: 'build it', workflow_name: '', project_id: 'p1',
      max_iterations: 3, runner: 'module', lead: { agent_id: 'lead-1' },
    }),
    tool: 'quicbaas_orchestrator_start_job',
    args: { description: 'build it', project_id: 'p1', max_iterations: 3, runner: 'module', lead: { agent_id: 'lead-1' } },
    // The server's shape: time.Unix(0, ns).UTC().Format(time.RFC3339Nano).
    reply: { job_id: 'job-1', timestamp: '2026-10-12T08:30:15.123456789Z' },
    want: { job_id: 'job-1', timestamp: Date.UTC(2026, 9, 12, 8, 30, 15, 123) * 1e6 },
  },
  {
    name: 'cancelJob',
    call: (o) => o.cancelJob('t1', 'job-1'),
    tool: 'quicbaas_orchestrator_cancel_job',
    args: { tenant_id: 't1', job_id: 'job-1' },
    reply: { cancelled: true, job_id: 'job-1' },
    want: undefined,
  },
  {
    name: 'listFleetNodes',
    call: (o) => o.listFleetNodes({ tenant_id: 't1', host: 'gpu-1.example', backend: 'ollama', limit: 5 }),
    tool: 'quicbaas_fleet_list',
    args: { tenant_id: 't1', host: 'gpu-1.example', backend: 'ollama', limit: 5 },
    reply: { nodes: [node], count: 1, total: 1 },
    want: { nodes: [node], count: 1 },
  },
  {
    name: 'getFleetNode',
    call: (o) => o.getFleetNode({ tenant_id: 't1', node_id: 'node-1' }),
    tool: 'quicbaas_fleet_status',
    args: { tenant_id: 't1', node_id: 'node-1' },
    reply: { node },
    want: node,
  },
  {
    name: 'registerFleetNode',
    call: (o) => o.registerFleetNode({ tenant_id: 't1', name: 'gpu-1', host: 'gpu-1.example', backend: 'ollama', node_type: 'daemon' }),
    tool: 'quicbaas_fleet_register',
    args: { tenant_id: 't1', name: 'gpu-1', host: 'gpu-1.example', backend: 'ollama', node_type: 'daemon' },
    reply: { node },
    want: { node },
  },
  {
    name: 'removeFleetNode',
    call: (o) => o.removeFleetNode({ tenant_id: 't1', node_id: 'node-1' }),
    tool: 'quicbaas_fleet_remove',
    args: { tenant_id: 't1', node_id: 'node-1' },
    reply: { removed: true, node_id: 'node-1' },
    want: undefined,
  },
  {
    name: 'disableFleetNode',
    call: (o) => o.disableFleetNode({ tenant_id: 't1', node_id: 'node-1' }),
    tool: 'quicbaas_fleet_disable',
    args: { tenant_id: 't1', node_id: 'node-1' },
    reply: { node_id: 'node-1', disabled: true, disabled_at: 'x', timestamp: 'y' },
    want: { node_id: 'node-1', disabled: true, disabled_at: 'x', timestamp: 'y' },
  },
  {
    name: 'enableFleetNode',
    call: (o) => o.enableFleetNode({ tenant_id: 't1', node_id: 'node-1' }),
    tool: 'quicbaas_fleet_enable',
    args: { tenant_id: 't1', node_id: 'node-1' },
    reply: { node_id: 'node-1', enabled: true, timestamp: 'y' },
    want: { node_id: 'node-1', enabled: true, timestamp: 'y' },
  },
  {
    name: 'healthCheck',
    call: (o) => o.healthCheck(),
    tool: 'quicbaas_orchestrator_health',
    args: {},
    reply: { total_sessions: 2, healthy: 1, unhealthy: 1, closed: 0, results: [], timestamp: '2026-10-12T00:00:00Z' },
    want: { total_sessions: 2, healthy: 1, unhealthy: 1, closed: 0, results: [], timestamp: Date.parse('2026-10-12T00:00:00Z') * 1e6 },
  },
  {
    name: 'listSessions',
    call: (o) => o.listSessions({ tenantID: 't1', status: 'active', job_id: 'job-1' }),
    tool: 'quicbaas_orchestrator_list_sessions',
    args: { tenant_id: 't1', status: 'active', job_id: 'job-1' },
    reply: { sessions: [{ session_id: 's1', backend: 'claude', state: 'active', created_at: 5, last_activity: 9, runner: 'module' }], total: 1 },
    want: { sessions: [{ session_id: 's1', backend: 'claude', status: 'active', created_at: 5, last_active: 9, runner: 'module' }], count: 1 },
  },
  {
    name: 'spawnSession',
    call: (o) => o.spawnSession({ tenantID: 't1', agent_id: 'a1', project_id: 'p1', sandbox_mode: 'none' as never }),
    tool: 'quicbaas_orchestrator_spawn',
    args: { tenant_id: 't1', agent_id: 'a1', project_id: 'p1', sandbox_mode: 'none' },
    reply: { session_id: 's1', backend: 'claude' },
    want: { session_id: 's1', backend: 'claude' },
  },
  {
    name: 'promptSession',
    call: (o) => o.promptSession({ tenant_id: 't1', session_id: 's1', prompt: 'hi' }),
    tool: 'quicbaas_orchestrator_prompt',
    args: { tenant_id: 't1', session_id: 's1', prompt: 'hi' },
    reply: { session_id: 's1', turn_id: 'turn-1', status: 'accepted' },
    want: { session_id: 's1', turn_id: 'turn-1', status: 'accepted' },
  },
  {
    name: 'closeSession',
    call: (o) => o.closeSession({ tenant_id: 't1', session_id: 's1' }),
    tool: 'quicbaas_orchestrator_close',
    args: { tenant_id: 't1', session_id: 's1' },
    reply: { session_id: 's1' },
    want: { session_id: 's1', closed: true },
  },
  {
    name: 'rotateSessionAccount',
    call: (o) => o.rotateSessionAccount({ tenant_id: 't1', session_id: 's1', reason: 'limit', hold_outgoing: true }),
    tool: 'quicbaas_orchestrator_rotate_account',
    args: { tenant_id: 't1', session_id: 's1', reason: 'limit', hold_outgoing: true },
    reply: { session_id: 's1', account_id: 'acct-2', mode: 'resume' },
    want: { session_id: 's1', account_id: 'acct-2', mode: 'resume' },
  },
  {
    name: 'getSessionHistory',
    call: (o) => o.getSessionHistory({ tenant_id: 't1', session_id: 's1', from_sequence: 4, limit: 10 }),
    tool: 'quicbaas_orchestrator_session_history',
    args: { tenant_id: 't1', session_id: 's1', from_sequence: 4, limit: 10 },
    reply: { events: [], next_seq: 0 },
    want: { events: [], next_seq: 0 },
  },
  {
    name: 'getSessionConversation',
    call: (o) => o.getSessionConversation({ tenant_id: 't1', session_id: 's1', cursor: 2 }),
    tool: 'quicbaas_orchestrator_session_conversation',
    args: { tenant_id: 't1', session_id: 's1', cursor: 2 },
    reply: { session_id: 's1', turns: [], next_cursor: 0 },
    want: { session_id: 's1', turns: [], next_cursor: 0 },
  },
  {
    name: 'getSpendReport',
    call: (o) => o.getSpendReport({ tenant_id: 't1', job_id: 'job-1', round: 0, round_set: true }),
    tool: 'quicbaas_orchestrator_cost',
    args: { tenant_id: 't1', job_id: 'job-1', round: 0 },
    reply: { total_tokens: 7 },
    want: { total_tokens: 7 },
  },
  {
    name: 'getTenantSpendPolicy',
    call: (o) => o.getTenantSpendPolicy({ tenant_id: 't1' }),
    tool: 'quicbaas_orchestrator_spend_policy_get',
    args: { tenant_id: 't1' },
    reply: policy,
    want: policy,
  },
  {
    name: 'setTenantSpendPolicy',
    call: (o) => o.setTenantSpendPolicy({ tenant_id: 't1', limits: policy.limits }),
    tool: 'quicbaas_orchestrator_spend_policy_set',
    args: { tenant_id: 't1', limits: policy.limits },
    reply: policy,
    want: policy,
  },
  {
    name: 'getCostSettings',
    call: (o) => o.getCostSettings({ tenant_id: 't1' }),
    tool: 'quicbaas_orchestrator_cost_settings_get',
    args: { tenant_id: 't1' },
    reply: costSettings,
    want: costSettings,
  },
  {
    name: 'setCostSettings',
    call: (o) => o.setCostSettings({ tenant_id: 't1', electricity_price_per_kwh: 0.2, engine_power_watts: { ollama: 300 } }),
    tool: 'quicbaas_orchestrator_cost_settings_set',
    args: { tenant_id: 't1', electricity_price_per_kwh: 0.2, engine_power_watts: { ollama: 300 } },
    reply: costSettings,
    want: costSettings,
  },
  {
    name: 'getWorkerImageStatus',
    call: (o) => o.getWorkerImageStatus(),
    tool: 'quicbaas_sdlc_worker_image',
    args: {},
    reply: workerImage,
    want: workerImage,
  },
  {
    name: 'refreshWorkerImage',
    call: (o) => o.refreshWorkerImage(),
    tool: 'quicbaas_sdlc_worker_image_refresh',
    args: {},
    reply: { ...workerImage, queued: true },
    want: { ...workerImage, queued: true },
  },
  {
    name: 'inference',
    call: (o) => o.inference({ backend: 'ollama', prompt: 'hi', model: 'm1', stream: true }),
    tool: 'quicbaas_orchestrator_inference',
    args: { backend: 'ollama', prompt: 'hi', model: 'm1' },
    reply: { content: [{ type: 'text', text: 'hello' }], text: 'hello', model: 'm1', stop_reason: 'end_turn', timestamp: 'x' },
    want: { content: [{ type: 'text', text: 'hello' }], model: 'm1', stop_reason: 'end_turn' },
  },
  {
    name: 'listWorkflows',
    call: (o) => o.listWorkflows('t1'),
    tool: 'quicbaas_orchestrator_list_workflows',
    args: { tenant_id: 't1' },
    reply: { workflows: [{ name: 'wf' }], count: 1 },
    want: { workflows: [{ name: 'wf' }], count: 1 },
  },
  {
    name: 'readmitTask',
    call: (o) => o.readmitTask('t1', 'task-1'),
    tool: 'quicbaas_orchestrator_task_readmit',
    args: { tenant_id: 't1', task_id: 'task-1' },
    reply: { task_id: 'task-1', readmitted: true },
    want: undefined,
  },
  {
    name: 'deleteTask',
    call: (o) => o.deleteTask({ tenant_id: 't1', task_id: 'task-1' }),
    tool: 'quicbaas_orchestrator_task_delete',
    args: { tenant_id: 't1', task_id: 'task-1' },
    reply: { task_id: 'task-1', deleted: true, timestamp: 'x' },
    want: { task_id: 'task-1', deleted: true },
  },
  {
    name: 'resolveAttention',
    call: (o) => o.resolveAttention({ tenant_id: 't1', task_id: 'task-1', action: 'answer', answer: 'yes' }),
    tool: 'quicbaas_orchestrator_attention_resolve',
    args: { tenant_id: 't1', task_id: 'task-1', action: 'answer', answer: 'yes' },
    reply: attentionItem,
    want: attentionItem,
  },
];

describe('McpClient.orchestrator — wired verbs', () => {
  it.each(wired)('$name sends its arguments to $tool and parses the reply', async (verb) => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, verb.reply)));
    const client = new McpClient(endpoint);

    const got = await verb.call(client.orchestrator);

    const calls = toolCalls(fetchMock);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.params?.name).toBe(verb.tool);
    expect(calls[0]?.params?.arguments).toEqual(verb.args);
    expect(got).toEqual(verb.want);
  });

  it.each(wired)('$name turns a $tool failure into McpError', async (verb) => {
    scriptServer(fetchMock, (msg) => jsonReply({
      jsonrpc: '2.0',
      id: msg.id,
      result: { content: [{ type: 'text', text: 'tenant_id is required' }], isError: true },
    }));
    const client = new McpClient(endpoint);

    const err = await verb.call(client.orchestrator).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).tool).toBe(verb.tool);
    expect((err as McpError).message).toContain('tenant_id is required');
  });
});

describe('McpClient.orchestrator — paged and composed verbs', () => {
  // listBackends and listRunners are unpaged verbs over paged tools: they
  // must walk next_offset to the end, as the REST transport does.
  it.each([
    { name: 'listBackends', tool: 'quicbaas_orchestrator_list_backends', key: 'backends', call: (o: Orchestrator) => o.listBackends() },
    { name: 'listRunners', tool: 'quicbaas_orchestrator_list_runners', key: 'runners', call: (o: Orchestrator) => o.listRunners() },
  ])('$name walks every page of $tool', async ({ tool, key, call }) => {
    scriptServer(fetchMock, (msg) => {
      const offset = (msg.params?.arguments?.['offset'] as number | undefined) ?? 0;
      const page = offset === 0
        ? { [key]: [{ name: 'a' }], next_offset: 1 }
        : { [key]: [{ name: 'b' }] };
      return jsonReply(toolText(msg.id as number, page));
    });
    const client = new McpClient(endpoint);

    const got = await call(client.orchestrator);

    expect(got).toEqual([{ name: 'a' }, { name: 'b' }]);
    const calls = toolCalls(fetchMock);
    expect(calls.map((c) => c.params?.name)).toEqual([tool, tool]);
    expect(calls.map((c) => c.params?.arguments)).toEqual([{ offset: 0 }, { offset: 1 }]);
  });

  // The paged listings forward the requested filters and page to the tool and
  // keep the server's page envelope, as the REST transport's page methods do.
  const pagedListings: Array<{
    name: string;
    tool: string;
    itemsKey: string;
    record: Record<string, unknown>;
    want: unknown;
    call: (o: McpClient['orchestrator'], offset: number) => Promise<PageResult<unknown>>;
    args: (offset: number) => Record<string, unknown>;
  }> = [
    {
      name: 'listSessionsPage',
      tool: 'quicbaas_orchestrator_list_sessions',
      itemsKey: 'sessions',
      record: { session_id: 's1', backend: 'claude', state: 'active', created_at: 5, last_activity: 9 },
      want: { session_id: 's1', backend: 'claude', status: 'active', created_at: 5, last_active: 9 },
      call: (o, offset) => o.listSessionsPage('t1', { status: 'active', runner: 'sdlc', limit: 25, offset }),
      args: (offset) => ({ tenant_id: 't1', status: 'active', runner: 'sdlc', limit: 25, offset }),
    },
    {
      name: 'listJobsPage',
      tool: 'quicbaas_orchestrator_list_jobs',
      itemsKey: 'jobs',
      record: { job_id: 'job-1', state: 'running' },
      want: { job_id: 'job-1', state: 'running' },
      call: (o, offset) => o.listJobsPage('t1', { task_id: 'task-1', statuses: ['running'], limit: 25, offset }),
      args: (offset) => ({ tenant_id: 't1', task_id: 'task-1', statuses: ['running'], limit: 25, offset }),
    },
    {
      name: 'listFleetNodesPage',
      tool: 'quicbaas_fleet_list',
      itemsKey: 'nodes',
      record: node,
      want: node,
      call: (o, offset) => o.listFleetNodesPage('t1', { backend: 'ollama', limit: 25, offset }),
      args: (offset) => ({ tenant_id: 't1', backend: 'ollama', limit: 25, offset }),
    },
  ];

  for (const tc of pagedListings) {
    it(`${tc.name} forwards the page and filters and keeps the server's next_offset`, async () => {
      scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {
        [tc.itemsKey]: [tc.record], total: 60, limit: 25, offset: 0, next_offset: 25,
      })));
      const client = new McpClient(endpoint);

      const page = await tc.call(client.orchestrator, 0);

      expect(toolCalls(fetchMock)[0]?.params?.name).toBe(tc.tool);
      expect(toolCalls(fetchMock)[0]?.params?.arguments).toEqual(tc.args(0));
      expect(page.items).toEqual([tc.want]);
      expect(page.total).toBe(60);
      expect(page.next_offset).toBe(25);
    });

    it(`${tc.name} sends the requested offset and reports no next_offset on the last page`, async () => {
      scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {
        [tc.itemsKey]: [tc.record], total: 51, limit: 25, offset: 50,
      })));
      const client = new McpClient(endpoint);

      const page = await tc.call(client.orchestrator, 50);

      expect(toolCalls(fetchMock)[0]?.params?.arguments).toEqual(tc.args(50));
      expect(page.offset).toBe(50);
      expect(page.total).toBe(51);
      expect(page.next_offset).toBeUndefined();
    });
  }

  it('listFleetNodesPage refuses a model filter the tool cannot apply', async () => {
    const client = new McpClient(endpoint);

    const err = await client.orchestrator.listFleetNodesPage('t1', { model: 'llama3' }).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TransportNotSupportedError);
    expect(toolCalls(fetchMock)).toHaveLength(0);
  });

  it('listBackends surfaces a tool failure as McpError', async () => {
    scriptServer(fetchMock, (msg) => jsonReply({
      jsonrpc: '2.0', id: msg.id,
      result: { content: [{ type: 'text', text: 'agent service unavailable' }], isError: true },
    }));
    const client = new McpClient(endpoint);

    const err = await client.orchestrator.listBackends().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).tool).toBe('quicbaas_orchestrator_list_backends');
  });

  it('listAttention sends the filters and adopts the items page', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {
      items: [attentionItem], total: 3, limit: 1, offset: 0, next_offset: 1,
    })));
    const client = new McpClient(endpoint);

    const page = await client.orchestrator.listAttention('t1', { kind: 'parked_task', limit: 1 });

    expect(toolCalls(fetchMock)[0]?.params?.arguments).toEqual({ tenant_id: 't1', kind: 'parked_task', limit: 1 });
    expect(page.items).toEqual([attentionItem]);
    expect(page.total).toBe(3);
    expect(page.next_offset).toBe(1);
  });

  it('listAttention surfaces a tool failure as McpError', async () => {
    scriptServer(fetchMock, (msg) => jsonReply({
      jsonrpc: '2.0', id: msg.id,
      result: { content: [{ type: 'text', text: 'sdlc unavailable' }], isError: true },
    }));
    const client = new McpClient(endpoint);

    const err = await client.orchestrator.listAttention('t1').catch((e: unknown) => e);

    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).tool).toBe('quicbaas_orchestrator_attention_list');
  });

  it('createTask sends the request flat and builds the Task from the reply', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {
      task_id: 'task-1', subject: 'fix it', status: 'pending', auto_spawn: true,
      spawned_job_id: 'job-9', runner: 'module', dispatch_reason: '',
    })));
    const client = new McpClient(endpoint);

    const resp = await client.orchestrator.createTask({
      tenant_id: 't1', subject: 'fix it', team_name: 'core', auto_spawn: true, metadata: { k: 'v' },
    });

    expect(toolCalls(fetchMock)[0]?.params?.arguments).toEqual({
      tenant_id: 't1', subject: 'fix it', team_name: 'core', auto_spawn: true, metadata: { k: 'v' },
    });
    expect(resp.task).toMatchObject({
      id: 'task-1', tenant_id: 't1', team_name: 'core', subject: 'fix it', status: 'pending',
      metadata: { k: 'v' }, auto_spawn: true, spawned_job_id: 'job-9', runner: 'module',
    });
    expect(resp.task.dispatch_reason).toBeUndefined();
  });

  // A task persisted but not dispatched comes back as a soft error field; the
  // REST route answers it as a failure, so this transport throws too.
  it('createTask throws McpError on the reply\'s dispatch error', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {
      task_id: 'task-1', subject: 'fix it', status: 'pending', auto_spawn: true, error: 'no matching agent',
    })));
    const client = new McpClient(endpoint);

    const err = await client.orchestrator.createTask({ tenant_id: 't1', subject: 'fix it', team_name: 'core' })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).tool).toBe('quicbaas_orchestrator_task_create');
    expect((err as McpError).message).toContain('no matching agent');
  });

  it('startJob answers timestamp 0 when the server instant does not parse', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { job_id: 'job-1', timestamp: 'not a time' })));
    const client = new McpClient(endpoint);

    const resp = await client.orchestrator.startJob({
      tenant_id: 't1', description: 'd', workflow_name: '', project_id: 'p1', max_iterations: 1,
    });

    expect(resp).toEqual({ job_id: 'job-1', timestamp: 0 });
  });

  it('getSpendReport leaves round off when round_set is false', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {})));
    const client = new McpClient(endpoint);

    await client.orchestrator.getSpendReport({ tenant_id: 't1', round: 2 });

    expect(toolCalls(fetchMock)[0]?.params?.arguments).toEqual({ tenant_id: 't1' });
  });
});

describe('McpClient.orchestrator — refusals', () => {
  // Each refusal must happen before anything reaches the wire.
  const refused: Array<{ name: string; call: (o: Orchestrator) => Promise<unknown> }> = [
    { name: 'listTasks', call: (o) => o.listTasks('t1') },
    { name: 'getTask', call: (o) => o.getTask('t1', 'task-1') },
    { name: 'claimTask', call: (o) => o.claimTask({ tenant_id: 't1', task_id: 'task-1' } as never) },
    { name: 'completeTask', call: (o) => o.completeTask({ tenant_id: 't1', task_id: 'task-1' } as never) },
    { name: 'createWorkflow', call: (o) => o.createWorkflow({} as never) },
    { name: 'getWorkflow', call: (o) => o.getWorkflow('t1', 'wf') },
    { name: 'updateWorkflow', call: (o) => o.updateWorkflow('wf', {} as never) },
    { name: 'deleteWorkflow', call: (o) => o.deleteWorkflow('t1', 'wf') },
    { name: 'decomposeTask', call: (o) => o.decomposeTask({} as never) },
    { name: 'listBackendModels', call: (o) => o.listBackendModels('node-1', 'ollama') },
    { name: 'startJob', call: (o) => o.startJob({ tenant_id: 't1', description: 'd', workflow_name: 'wf', project_id: 'p', max_iterations: 1 }) },
    { name: 'startJob', call: (o) => o.startJob({ tenant_id: 't1', description: 'd', workflow_name: '', project_id: 'p', max_iterations: 1, poll_interval: '5s' }) },
    { name: 'listFleetNodes', call: (o) => o.listFleetNodes({ tenant_id: 't1', model: 'm1' }) },
    { name: 'registerFleetNode', call: (o) => o.registerFleetNode({ tenant_id: 't1', name: 'n', host: 'h', backend: 'b', node_type: 'daemon', model: 'm1' }) },
    { name: 'registerFleetNode', call: (o) => o.registerFleetNode({ tenant_id: 't1', name: 'n', host: 'h', backend: 'b', node_type: 'daemon', metadata: { k: 'v' } }) },
    { name: 'spawnSession', call: (o) => o.spawnSession({ tenantID: 't1', env: { K: 'v' } }) },
    { name: 'spawnSession', call: (o) => o.spawnSession({ tenantID: 't1', sandboxed: true }) },
    { name: 'inference', call: (o) => o.inference({ backend: 'b', prompt: 'p', host_affinity_strict: true }) },
    { name: 'createTask', call: (o) => o.createTask({ tenant_id: 't1', subject: 's', team_name: 'c', host_affinity_strict: true }) },
  ];

  it.each(refused)('$name refuses with TransportNotSupportedError', async ({ name, call }) => {
    const client = new McpClient(endpoint);

    const err = await call(client.orchestrator).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TransportNotSupportedError);
    expect((err as TransportNotSupportedError).transport).toBe('mcp');
    expect((err as TransportNotSupportedError).method).toBe(name);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('subscribeJobEvents refuses synchronously', () => {
    const client = new McpClient(endpoint);

    expect(() => client.orchestrator.subscribeJobEvents('t1', 'job-1', () => undefined))
      .toThrow(TransportNotSupportedError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([
    { name: 'subscribeSession', open: (o: Orchestrator) => o.subscribeSession({ session_id: 's1' } as never) },
    { name: 'inferenceStream', open: (o: Orchestrator) => o.inferenceStream({ backend: 'b', prompt: 'p' }) },
  ])('$name refuses on first read', async ({ name, open }) => {
    const client = new McpClient(endpoint);
    const iterator = open(client.orchestrator)[Symbol.asyncIterator]();

    const err = await iterator.next().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TransportNotSupportedError);
    expect((err as TransportNotSupportedError).method).toBe(name);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('McpClient — tool results and 64-bit integers', () => {
  // A tool answers with the JSON the REST route returns, so a page record's
  // 64-bit identity must survive exactly as it does over REST (#555), while a
  // UnixNano quantity stays a number.
  it('keeps a page record id past 2^53 as its exact decimal string', async () => {
    const text = '{"jobs":[{"id":1788567351228350294,"created_at":1788567351228350294}],"total":1}';
    scriptServer(fetchMock, (msg) =>
      jsonReply({ jsonrpc: '2.0', id: msg.id, result: { content: [{ type: 'text', text }] } }),
    );
    const client = new McpClient(endpoint);

    const page = await client.orchestrator.listJobsPage('t1', { limit: 1 });

    const record = page.items[0] as unknown as Record<string, unknown>;
    expect(record['id']).toBe('1788567351228350294');
    expect(typeof record['created_at']).toBe('number');
  });

  it('rejects a tool result that is not JSON', async () => {
    scriptServer(fetchMock, (msg) =>
      jsonReply({ jsonrpc: '2.0', id: msg.id, result: { content: [{ type: 'text', text: '{"jobs":[' }] } }),
    );
    const client = new McpClient(endpoint);

    const err = await client.orchestrator.listJobsPage('t1', { limit: 1 }).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(SyntaxError);
  });
});
