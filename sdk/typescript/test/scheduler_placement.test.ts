// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The scheduler's workload placement surface over REST: each method must hit
// the route the Go server serves (pkg/api/rest handlers_scheduler_placement.go)
// with the body it reads, and unwrap the envelope it returns.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
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

const node = {
  facts: { node_id: 'n1', capacity: { cpu: 8 }, labels: { zone: 'a' } },
  status: { unschedulable: false, draining: false },
  lease_id: 7,
  ttl_seconds: 30,
  placements: 0,
  registered_at: '2026-10-01T00:00:00Z',
};
const placement = { workload_id: 'w1', node_id: 'n1', score: 37.5, created_at: '2026-10-01T00:00:00Z' };

function lastCall(): { url: string; method: string; body?: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit];
  return { url, method: init?.method ?? 'GET', body: init?.body ? JSON.parse(init.body as string) : undefined };
}

describe('RestSchedulerClient placement', () => {
  it('registerNode posts the facts and ttl', async () => {
    fetchMock.mockResolvedValue(makeResponse(node));
    const client = new RestClient('http://localhost');
    const got = await client.scheduler.registerNode({ facts: node.facts, ttl_seconds: 30 });
    expect(got.lease_id).toBe(7);
    expect(lastCall()).toEqual({
      url: 'http://localhost/api/v1/scheduler/nodes',
      method: 'POST',
      body: { facts: node.facts, ttl_seconds: 30 },
    });
  });

  it('node verbs hit their routes with an escaped id', async () => {
    const client = new RestClient('http://localhost');
    const cases: Array<[() => Promise<unknown>, string, string, unknown]> = [
      [() => client.scheduler.renewNode('n/1'), 'nodes/n%2F1/renew', 'POST', { node_id: 'n/1', renewed: true }],
      [() => client.scheduler.deregisterNode('n1'), 'nodes/n1', 'DELETE', { node_id: 'n1', deregistered: true }],
      [() => client.scheduler.getNode('n1'), 'nodes/n1', 'GET', node],
      [() => client.scheduler.cordonNode('n1'), 'nodes/n1/cordon', 'POST', node],
      [() => client.scheduler.uncordonNode('n1'), 'nodes/n1/uncordon', 'POST', node],
    ];
    for (const [call, path, method, body] of cases) {
      fetchMock.mockResolvedValue(makeResponse(body));
      await call();
      expect(lastCall().url).toBe(`http://localhost/api/v1/scheduler/${path}`);
      expect(lastCall().method).toBe(method);
    }
  });

  it('listNodes and drainNode unwrap their envelopes, null as empty', async () => {
    const client = new RestClient('http://localhost');
    fetchMock.mockResolvedValue(makeResponse({ nodes: [node] }));
    expect(await client.scheduler.listNodes()).toHaveLength(1);
    fetchMock.mockResolvedValue(makeResponse({ nodes: null }));
    expect(await client.scheduler.listNodes()).toEqual([]);
    fetchMock.mockResolvedValue(makeResponse({ node_id: 'n1', placements: [placement] }));
    expect(await client.scheduler.drainNode('n1')).toEqual([placement]);
    fetchMock.mockResolvedValue(makeResponse({ node_id: 'n1', placements: null }));
    expect(await client.scheduler.drainNode('n1')).toEqual([]);
  });

  it('place posts the request; listPlacements filters by query', async () => {
    const client = new RestClient('http://localhost');
    fetchMock.mockResolvedValue(makeResponse({ placement, existing: false }));
    const req = {
      workload_id: 'w1',
      group: 'web',
      resources: { cpu: 2 },
      constraints: [{ key: 'zone', operator: 'In' as const, values: ['a'] }],
      strategy: 'spread' as const,
    };
    const res = await client.scheduler.place(req);
    expect(res.placement.node_id).toBe('n1');
    expect(lastCall()).toEqual({ url: 'http://localhost/api/v1/scheduler/placements', method: 'POST', body: req });

    fetchMock.mockResolvedValue(makeResponse({ placements: [placement] }));
    expect(await client.scheduler.listPlacements({ node_id: 'n1', group: 'web' })).toHaveLength(1);
    expect(lastCall().url).toBe('http://localhost/api/v1/scheduler/placements?node_id=n1&group=web');
    fetchMock.mockResolvedValue(makeResponse({ placements: null }));
    expect(await client.scheduler.listPlacements()).toEqual([]);
    expect(lastCall().url).toBe('http://localhost/api/v1/scheduler/placements');

    fetchMock.mockResolvedValue(makeResponse(placement));
    expect((await client.scheduler.getPlacement('w1')).workload_id).toBe('w1');
    fetchMock.mockResolvedValue(makeResponse({ workload_id: 'w1', released: true }));
    await client.scheduler.release('w1');
    expect(lastCall()).toEqual({ url: 'http://localhost/api/v1/scheduler/placements/w1', method: 'DELETE', body: undefined });
  });

  it('surfaces a server refusal as an error', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'no feasible node for workload w' }, 412));
    const client = new RestClient('http://localhost');
    await expect(client.scheduler.place({ workload_id: 'w' })).rejects.toThrow('no feasible node');
  });
});
