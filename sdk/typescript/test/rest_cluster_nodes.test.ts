// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
// 2. Commercial License

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';
import type { ListClusterNodesResponse } from '../src/types.js';

const sample: ListClusterNodesResponse = {
  nodes: [
    {
      node_id: 1,
      address: '10.0.0.1:8443',
      raft_address: '10.0.0.1:7000',
      is_leader: true,
      health_status: 'healthy',
      version: 'v1.2.3',
      uptime_seconds: 3600,
    },
    {
      node_id: 2,
      address: '10.0.0.2:8443',
      raft_address: '10.0.0.2:7000',
      is_leader: false,
      health_status: 'healthy',
      version: 'v1.2.3',
      uptime_seconds: 60,
    },
  ],
  total_count: 2,
  timestamp: '2026-11-21T00:00:00Z',
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

describe('RestClusterClient.listNodes', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('GETs /api/v1/cluster/nodes and returns the body', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(sample));
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    const resp = await client.cluster.listNodes();

    expect(resp).toEqual(sample);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [RequestInfo | URL, RequestInit | undefined];
    expect(String(url).endsWith('/api/v1/cluster/nodes')).toBe(true);
    expect(init?.method).toBe('GET');
  });

  it('returns nodes as [] when the server sends null', async () => {
    globalThis.fetch = vi.fn(async () =>
      jsonResponse({ nodes: null, total_count: 0, timestamp: sample.timestamp }),
    ) as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    const resp = await client.cluster.listNodes();

    expect(resp.nodes).toEqual([]);
    expect(resp.total_count).toBe(0);
  });

  it('rejects on a 500', async () => {
    globalThis.fetch = vi.fn(async () =>
      jsonResponse({ error: 'boom' }, 500),
    ) as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    await expect(client.cluster.listNodes()).rejects.toThrow();
  });
});
