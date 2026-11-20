// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestVectorAdminClient (/api/v1/vector/admin/*): URL, method and
// body of each call, JSON Lines export parsing and import encoding, and the
// error path.

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

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function call(i = 0): { url: string; init: RequestInit & { headers: Record<string, string> } } {
  const [url, init] = fetchMock.mock.calls[i];
  return { url: url as string, init: init as RequestInit & { headers: Record<string, string> } };
}

describe('RestVectorAdminClient', () => {
  it('getGlobalStats GETs the global stats route', async () => {
    fetchMock.mockResolvedValue(json({ cluster_id: 'c1', total_nodes: 3 }));
    const client = new RestClient('http://localhost');
    const stats = await client.vectorAdmin.getGlobalStats();
    const { url, init } = call();
    expect(url).toBe('http://localhost/api/v1/vector/admin/stats/global');
    expect(init.method ?? 'GET').toBe('GET');
    expect(init.body).toBeUndefined();
    expect(stats.cluster_id).toBe('c1');
  });

  it('compactNamespace POSTs the namespace', async () => {
    fetchMock.mockResolvedValue(json({ operation_id: 'op1', started: true }, 202));
    const client = new RestClient('http://localhost');
    const resp = await client.vectorAdmin.compactNamespace('team/a');
    const { url, init } = call();
    expect(url).toBe('http://localhost/api/v1/vector/admin/compact');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ namespace: 'team/a' });
    expect(resp).toEqual({ operation_id: 'op1', started: true });
  });

  it('getCompactionProgress encodes the operation id', async () => {
    fetchMock.mockResolvedValue(json({ operation_id: 'op/1', status: 'running' }));
    const client = new RestClient('http://localhost');
    const p = await client.vectorAdmin.getCompactionProgress('op/1');
    const { url, init } = call();
    expect(url).toBe('http://localhost/api/v1/vector/admin/compact/progress?operation_id=op%2F1');
    expect(init.method ?? 'GET').toBe('GET');
    expect(p.status).toBe('running');
  });

  it('exportVectors POSTs the request and parses two JSON lines', async () => {
    const body =
      '{"id":"v1","vector":[1,2],"metadata":{"k":"v"}}\n\n{"id":"v2","vector":[3]}\n';
    fetchMock.mockResolvedValue(new Response(body, { status: 200 }));
    const client = new RestClient('http://localhost');
    const entries = await client.vectorAdmin.exportVectors('team/a', {
      batch_size: 10,
      metadata_filters: { k: 'v' },
    });
    const { url, init } = call();
    expect(url).toBe('http://localhost/api/v1/vector/admin/export/team%2Fa');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ batch_size: 10, metadata_filters: { k: 'v' } });
    expect(entries).toEqual([
      { id: 'v1', vector: [1, 2], metadata: { k: 'v' } },
      { id: 'v2', vector: [3] },
    ]);
  });

  it('importVectors sends two ndjson lines', async () => {
    fetchMock.mockResolvedValue(
      json({ vectors_imported: 2, vectors_skipped: 0, vectors_failed: 0, duration_ms: 5, timestamp: 't' }),
    );
    const client = new RestClient('http://localhost');
    const resp = await client.vectorAdmin.importVectors('team/a', [
      { id: 'v1', vector: [1, 2] },
      { id: 'v2', vector: [3], metadata: { k: 'v' } },
    ]);
    const { url, init } = call();
    expect(url).toBe('http://localhost/api/v1/vector/admin/import/team%2Fa');
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/x-ndjson');
    const lines = (init.body as string).split('\n');
    expect(lines).toHaveLength(2);
    expect(JSON.parse(lines[0])).toEqual({ id: 'v1', vector: [1, 2] });
    expect(JSON.parse(lines[1])).toEqual({ id: 'v2', vector: [3], metadata: { k: 'v' } });
    expect(resp.vectors_imported).toBe(2);
  });

  it('a 403 rejects', async () => {
    fetchMock.mockResolvedValue(json({ error: 'forbidden' }, 403));
    const client = new RestClient('http://localhost');
    await expect(client.vectorAdmin.exportVectors('ns')).rejects.toMatchObject({ status: 403 });
  });
});
