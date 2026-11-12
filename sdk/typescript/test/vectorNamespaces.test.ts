// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The vectors client reads a namespace's info and stats, rebuilds its index
// and reads the rebuild's progress: each method's URL, HTTP method and body.

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

function respond(status: number, payload: unknown) {
  const body = JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 403 ? 'Forbidden' : 'OK',
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

function call(i = 0): { url: string; init: RequestInit } {
  const [url, init] = fetchMock.mock.calls[i] as [string, RequestInit];
  return { url, init: init ?? {} };
}

const base = 'http://localhost';
const ns = 'team/docs';
const enc = 'team%2Fdocs';

describe('vectors namespace info, stats and index', () => {
  it('getNamespace GETs the namespace path, encoding the namespace', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { namespace: ns, dimensions: 3, distance_metric: 'cosine', index_type: 'hnsw', vector_count: 2, indexed_count: 2, storage_bytes: 10, index_bytes: 5 }),
    );
    const info = await new RestClient(base).vectors.getNamespace(ns);
    const { url, init } = call();
    expect(url).toBe(`${base}/api/v1/vector/namespaces/${enc}`);
    expect(init.method ?? 'GET').toBe('GET');
    expect(init.body).toBeUndefined();
    expect(info.dimensions).toBe(3);
    expect(info.index_type).toBe('hnsw');
  });

  it('getNamespaceStats GETs the stats path', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { namespace: ns, vector_count: 2, indexed_count: 2, storage_bytes: 10, index_bytes: 5, avg_search_latency_ms: 1.5, search_count: 7 }),
    );
    const stats = await new RestClient(base).vectors.getNamespaceStats(ns);
    const { url, init } = call();
    expect(url).toBe(`${base}/api/v1/vector/namespaces/${enc}/stats`);
    expect(init.method ?? 'GET').toBe('GET');
    expect(init.body).toBeUndefined();
    expect(stats.search_count).toBe(7);
  });

  it('rebuildIndex POSTs the request body to the rebuild path', async () => {
    fetchMock.mockResolvedValue(
      respond(202, { operation_id: 'op-1', started: '2026-11-12T00:00:00Z', estimated_duration_ms: 1000 }),
    );
    const req = { index_params: { m: '16' }, async: true, timeout_ms: 5000 };
    const res = await new RestClient(base).vectors.rebuildIndex(ns, req);
    const { url, init } = call();
    expect(url).toBe(`${base}/api/v1/vector/namespaces/${enc}/index/rebuild`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual(req);
    expect(res.operation_id).toBe('op-1');
  });

  it('getIndexProgress GETs the progress path with the operation_id query', async () => {
    fetchMock.mockResolvedValue(
      respond(200, { operation_id: 'op 1/2', namespace: ns, vectors_indexed: 1, total_vectors: 2, percent_complete: 50, elapsed_time_ms: 10, estimated_remaining_ms: 10, status: 'running' }),
    );
    const p = await new RestClient(base).vectors.getIndexProgress(ns, 'op 1/2');
    const { url, init } = call();
    expect(url).toBe(`${base}/api/v1/vector/namespaces/${enc}/index/progress?operation_id=op%201%2F2`);
    expect(init.method ?? 'GET').toBe('GET');
    expect(p.percent_complete).toBe(50);
  });

  it('rejects on a 403', async () => {
    fetchMock.mockResolvedValue(respond(403, { error: 'forbidden' }));
    await expect(new RestClient(base).vectors.getNamespaceStats(ns)).rejects.toThrow('forbidden');
  });
});
