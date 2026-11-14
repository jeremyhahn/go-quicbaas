// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The prompts family (#812): the layered prompt library over REST
// (/api/v1/prompts/..., pkg/api/rest/routes.go) and MCP (the prompt_* tools,
// pkg/api/mcp/prompt_tools.go). Each verb must reach the route or tool the
// server serves, with the scope as query (REST) or flat arguments (MCP).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { McpClient } from '../src/mcp';
import { QuicBaaSError } from '../src/errors';
import { jsonReply, scriptServer, toolText, type RpcMessage } from './mcp_server';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

const record = { tenant_id: 'tenant-a', scope: 'tenant', set: 'sdlc', name: 'plan', version: 3 };

function restReply(): void {
  fetchMock.mockImplementation(async () => new Response(JSON.stringify(record), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }));
}

function restCalls(): Array<{ url: string; method: string; body: unknown }> {
  return fetchMock.mock.calls.map((call) => {
    const init = (call[1] ?? {}) as RequestInit;
    return {
      url: String(call[0]),
      method: init.method ?? 'GET',
      body: init.body ? JSON.parse(init.body as string) : undefined,
    };
  });
}

describe('RestClient.prompts — the /api/v1/prompts routes', () => {
  it('maps each verb to its route, method, query and body', async () => {
    restReply();
    const p = new RestClient('http://localhost:8080').prompts;
    const addr = { tenant_id: 'tenant-a', scope: 'project', project_id: 'proj-1', set: 'sdlc', name: 'plan' };

    const got = await p.getPrompt({ ...addr, version: 2 });
    await p.listPrompts({ set: 'sdlc', scope: 'tenant' });
    await p.getEffectivePrompt(addr);
    await p.diffPrompt(addr);
    await p.getPromptHistory(addr);
    await p.putPrompt({ ...addr, content: 'body', note: 'edit', expected_version: 3 });
    await p.rollbackPrompt({ ...addr, to_version: 1, expected_version: 4 });
    await p.resetPrompt({ ...addr, note: 'back', expected_version: 5 });

    expect(got.version).toBe(3);
    const q = 'tenant_id=tenant-a&scope=project&project_id=proj-1';
    const base = 'http://localhost:8080/api/v1/prompts/sdlc';
    expect(restCalls()).toEqual([
      { url: `${base}/plan?${q}&version=2`, method: 'GET', body: undefined },
      { url: `${base}?scope=tenant`, method: 'GET', body: undefined },
      { url: `${base}/plan/effective?${q}`, method: 'GET', body: undefined },
      { url: `${base}/plan/diff?${q}`, method: 'GET', body: undefined },
      { url: `${base}/plan/history?${q}`, method: 'GET', body: undefined },
      { url: `${base}/plan?${q}`, method: 'PUT', body: { content: 'body', note: 'edit', expected_version: 3 } },
      { url: `${base}/plan/rollback?${q}`, method: 'POST', body: { to_version: 1, note: '', expected_version: 4 } },
      { url: `${base}/plan?${q}&expected_version=5&note=back`, method: 'DELETE', body: undefined },
    ]);
  });

  it('refuses a one-prompt verb without a name before any request', async () => {
    restReply();
    const p = new RestClient('http://localhost:8080').prompts;
    const err = await p.getEffectivePrompt({ set: 'sdlc' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(QuicBaaSError);
    expect((err as Error).message).toBe('prompts.getEffectivePrompt: name is required');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('McpClient.prompts — the prompt_* tools', () => {
  it('maps each verb to its tool and passes the request flat', async () => {
    const seen: string[] = [];
    scriptServer(fetchMock, (msg) => {
      seen.push(msg.params?.name as string);
      return jsonReply(toolText(msg.id as number, record));
    });
    const p = new McpClient('http://node.example:8081/mcp').prompts;
    const addr = { tenant_id: 'tenant-a', set: 'sdlc', name: 'plan' };

    const got = await p.getPrompt({ ...addr, version: 2 });
    await p.listPrompts({ set: 'sdlc' });
    await p.getEffectivePrompt(addr);
    await p.diffPrompt(addr);
    await p.getPromptHistory(addr);
    await p.putPrompt({ ...addr, content: 'body', note: 'edit', expected_version: 3 });
    await p.rollbackPrompt({ ...addr, to_version: 1, expected_version: 4 });
    await p.resetPrompt({ ...addr, expected_version: 5 });

    expect(got.version).toBe(3);
    expect(seen).toEqual([
      'prompt_get', 'prompt_list', 'prompt_effective', 'prompt_diff',
      'prompt_history', 'prompt_put', 'prompt_rollback', 'prompt_reset',
    ]);
    const args = fetchMock.mock.calls
      .map((call) => JSON.parse((call[1] as RequestInit).body as string) as RpcMessage)
      .filter((m) => m.method === 'tools/call')
      .map((m) => m.params?.arguments);
    expect(args[0]).toEqual({ ...addr, version: 2 });
    expect(args[5]).toEqual({ ...addr, content: 'body', note: 'edit', expected_version: 3 });
    expect(args[6]).toEqual({ ...addr, to_version: 1, expected_version: 4 });
  });
});
