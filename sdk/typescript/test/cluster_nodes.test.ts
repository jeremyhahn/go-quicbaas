// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestClusterNodesClient (client.clusterNodes): each method's URL,
// HTTP method and body against /api/v1/cluster/nodes/*.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function respond(status: number, payload: unknown): {
  ok: boolean;
  status: number;
  statusText: string;
  headers: Headers;
  json: () => Promise<unknown>;
  text: () => Promise<string>;
} {
  const body = JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 403 ? 'Forbidden' : 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

function lastCall(): { url: string; method: string; body: unknown } {
  const [input, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [
    string | URL | Request,
    RequestInit | undefined,
  ];
  const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
  const raw = init?.body;
  return {
    url,
    method: (init?.method ?? 'GET').toUpperCase(),
    body: typeof raw === 'string' ? (JSON.parse(raw) as unknown) : raw,
  };
}

const now = '2026-11-12T00:00:00Z';
const list = {
  nodes: [
    {
      node_id: 1,
      address: 'n1:9000',
      raft_address: 'n1:9001',
      is_leader: true,
      health_status: 'healthy',
      version: 'v1',
      uptime_seconds: 10,
    },
  ],
  total_count: 1,
  timestamp: now,
};

describe('RestClusterNodesClient', () => {
  it('listNodes GETs the list path with its trailing slash', async () => {
    fetchMock.mockResolvedValue(respond(200, list));
    const client = new RestClient('http://localhost');
    const res = await client.clusterNodes.listNodes();
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/cluster/nodes/');
    expect(call.method).toBe('GET');
    expect(call.body).toBeUndefined();
    expect(res.nodes[0].node_id).toBe(1);
    expect(res.total_count).toBe(1);
  });

  it('addNode POSTs the request to the path with its trailing slash', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { success: true, message: 'added', shard_id: 2, node_id: 5, timestamp: now }),
    );
    const client = new RestClient('http://localhost');
    const req = { shard_id: 2, node_id: 5, address: 'n5:9000', identity: 'spiffe://x/n5' };
    const res = await client.clusterNodes.addNode(req);
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/cluster/nodes/');
    expect(call.method).toBe('POST');
    expect(call.body).toEqual(req);
    expect(res.success).toBe(true);
    expect(res.node_id).toBe(5);
  });

  it('listNonLeaderNodes GETs the non-leader path', async () => {
    fetchMock.mockResolvedValue(respond(200, { ...list, nodes: [] }));
    const client = new RestClient('http://localhost');
    const res = await client.clusterNodes.listNonLeaderNodes();
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/cluster/nodes/non-leader');
    expect(call.method).toBe('GET');
    expect(res.nodes).toEqual([]);
  });

  it('removeNode DELETEs the node with the shard_id query', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { success: true, message: 'removed', shard_id: 3, node_id: 7, timestamp: now }),
    );
    const client = new RestClient('http://localhost');
    const res = await client.clusterNodes.removeNode(7, 3);
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/cluster/nodes/7?shard_id=3');
    expect(call.method).toBe('DELETE');
    expect(call.body).toBeUndefined();
    expect(res.shard_id).toBe(3);
  });

  it('removeNode encodes a node id and shard id containing "/"', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { success: true, message: 'removed', shard_id: 3, node_id: 7, timestamp: now }),
    );
    const client = new RestClient('http://localhost');
    await client.clusterNodes.removeNode('a/b' as unknown as number, '1/2' as unknown as number);
    expect(lastCall().url).toBe('http://localhost/api/v1/cluster/nodes/a%2Fb?shard_id=1%2F2');
  });

  it('replaceNode PUTs the request to the replace path, encoding the node id', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { success: true, message: 'replaced', affected_shards: [1, 2], timestamp: now }),
    );
    const client = new RestClient('http://localhost');
    const req = { replacement_node_id: 9, replacement_address: 'n9:9000' };
    const res = await client.clusterNodes.replaceNode('x/y' as unknown as number, req);
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/cluster/nodes/x%2Fy/replace');
    expect(call.method).toBe('PUT');
    expect(call.body).toEqual(req);
    expect(res.affected_shards).toEqual([1, 2]);
  });

  it('replaceNode PUTs a numeric node id unchanged', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { success: true, message: 'replaced', affected_shards: [], timestamp: now }),
    );
    const client = new RestClient('http://localhost');
    await client.clusterNodes.replaceNode(4, { replacement_node_id: 9, replacement_address: 'n9:9000' });
    expect(lastCall().url).toBe('http://localhost/api/v1/cluster/nodes/4/replace');
  });

  it('a 403 rejects', async () => {
    fetchMock.mockResolvedValue(respond(403, { error: 'forbidden' }));
    const client = new RestClient('http://localhost');
    await expect(client.clusterNodes.listNodes()).rejects.toThrow();
  });
});
