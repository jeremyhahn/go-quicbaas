// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The tenant AI agent catalog on the wire (pkg/api/rest/handlers_agents.go):
// every verb's method, route and body, the list envelopes unwrapped, and a
// refusal surfaced as an APIError.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import { formatAgentRef, parseAgentRef } from '../src/agents';
import type { Agent, AgentPool } from '../src/agents';

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

const agent: Agent = {
  id: 'codex-reviewer',
  name: 'Codex reviewer',
  roles: ['review'],
  backend: 'codex',
  model: 'gpt-5.5',
  reasoning_effort: 'high',
};
const pool: AgentPool = { id: 'reviewers', name: 'Reviewers', agents: ['codex-reviewer', 'qwen-reviewer'] };

describe('RestAgentsClient', () => {
  it('sends every Agent verb to its route', async () => {
    const agents = new RestClient('http://localhost').agents;
    const cases: Array<{ run: () => Promise<unknown>; body: unknown; url: string; method: string; sent?: unknown }> = [
      { run: () => agents.listAgents('acme'), body: { agents: [agent] }, url: '/api/v1/agents?tenant_id=acme', method: 'GET' },
      { run: () => agents.getAgent('codex reviewer'), body: agent, url: '/api/v1/agents/codex%20reviewer', method: 'GET' },
      { run: () => agents.createAgent(agent, 'acme'), body: agent, url: '/api/v1/agents?tenant_id=acme', method: 'POST', sent: agent },
      { run: () => agents.updateAgent('codex-reviewer', agent), body: agent, url: '/api/v1/agents/codex-reviewer', method: 'PUT', sent: agent },
      { run: () => agents.deleteAgent('codex-reviewer'), body: { deleted: true }, url: '/api/v1/agents/codex-reviewer', method: 'DELETE' },
      { run: () => agents.agentUsedBy('codex-reviewer'), body: { used_by: [] }, url: '/api/v1/agents/codex-reviewer/used-by', method: 'GET' },
      { run: () => agents.listPools(), body: { pools: [pool] }, url: '/api/v1/agent-pools', method: 'GET' },
      { run: () => agents.getPool('reviewers'), body: pool, url: '/api/v1/agent-pools/reviewers', method: 'GET' },
      { run: () => agents.createPool(pool), body: pool, url: '/api/v1/agent-pools', method: 'POST', sent: pool },
      { run: () => agents.updatePool('reviewers', pool, 'acme'), body: pool, url: '/api/v1/agent-pools/reviewers?tenant_id=acme', method: 'PUT', sent: pool },
      { run: () => agents.deletePool('reviewers'), body: { deleted: true }, url: '/api/v1/agent-pools/reviewers', method: 'DELETE' },
      { run: () => agents.poolUsedBy('reviewers'), body: { used_by: [] }, url: '/api/v1/agent-pools/reviewers/used-by', method: 'GET' },
    ];
    for (const tc of cases) {
      fetchMock.mockReset();
      fetchMock.mockResolvedValue(makeResponse(tc.body));
      await tc.run();
      const [url, init] = fetchMock.mock.calls[0];
      expect(url).toBe(`http://localhost${tc.url}`);
      expect(init.method ?? 'GET').toBe(tc.method);
      if (tc.sent !== undefined) expect(JSON.parse(init.body)).toEqual(tc.sent);
    }
  });

  it('unwraps the list envelopes, and an absent list is empty', async () => {
    const agents = new RestClient('http://localhost').agents;
    fetchMock.mockResolvedValueOnce(makeResponse({ agents: [agent] }));
    expect(await agents.listAgents()).toEqual([agent]);
    fetchMock.mockResolvedValueOnce(makeResponse({ pools: [pool] }));
    expect((await agents.listPools())[0].agents).toEqual(['codex-reviewer', 'qwen-reviewer']);
    fetchMock.mockResolvedValueOnce(makeResponse({ used_by: [{ kind: 'binding', id: 'p1', slot: 'reviewer:default' }] }));
    expect((await agents.agentUsedBy('codex-reviewer'))[0].slot).toBe('reviewer:default');
    fetchMock.mockResolvedValueOnce(makeResponse({}));
    expect(await agents.poolUsedBy('reviewers')).toEqual([]);
    fetchMock.mockResolvedValueOnce(makeResponse({}));
    expect(await agents.listAgents()).toEqual([]);
  });

  it('surfaces a refusal to delete an Agent in use', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'agent codex-reviewer is in use by binding p1 (worker)' }, 409));
    await expect(new RestClient('http://localhost').agents.deleteAgent('codex-reviewer')).rejects.toBeInstanceOf(APIError);
  });
});

describe('agent references', () => {
  it('parses and formats agent and pool references', () => {
    expect(parseAgentRef('')).toEqual({});
    expect(parseAgentRef('claude-coder')).toEqual({ agent_id: 'claude-coder' });
    expect(parseAgentRef(' agent: codex ')).toEqual({ agent_id: 'codex' });
    expect(parseAgentRef('pool:coders')).toEqual({ pool_id: 'coders' });
    expect(() => parseAgentRef('fleet:node-1')).toThrow();
    expect(formatAgentRef({ pool_id: 'coders' })).toBe('pool:coders');
    expect(formatAgentRef({ agent_id: 'codex' })).toBe('agent:codex');
    expect(formatAgentRef(undefined)).toBe('');
  });
});
