// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for the vectors client's health, cluster-stats, hybrid-search and
// search-by-metadata methods: each sends the right URL, HTTP method and
// body, returns the reply, and rejects on a 403.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type {
  VectorHybridSearchRequest,
  VectorMetadataSearchRequest,
} from '../src/vector_search';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function make200JSON(payload: unknown) {
  const body = JSON.stringify(payload);
  return {
    ok: true,
    status: 200,
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

function make403() {
  return {
    ok: false,
    status: 403,
    statusText: 'Forbidden',
    json: async () => ({ error: 'permission denied' }),
    text: async () => '{"error":"permission denied"}',
  };
}

function lastCall(): { url: string; init: RequestInit } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit];
  return { url, init: init ?? {} };
}

describe('VectorsClient health / clusterStats / hybridSearch / searchByMetadata', () => {
  it('health GETs /api/v1/vector/health and returns the reply', async () => {
    const reply = {
      status: 'healthy',
      node_id: 1,
      version: '1.0.0',
      uptime_seconds: 42,
      namespaces_healthy: 2,
      namespaces_degraded: 0,
      namespaces_unhealthy: 0,
      details: { index: { status: 'healthy', last_check: '2026-11-12T00:00:00Z' } },
    };
    fetchMock.mockResolvedValue(make200JSON(reply));
    const client = new RestClient('http://localhost');
    const got = await client.vectors.health();
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/vector/health');
    expect(init.method ?? 'GET').toBe('GET');
    expect(init.body).toBeUndefined();
    expect(got).toEqual(reply);
  });

  it('clusterStats GETs /api/v1/vector/cluster/stats and returns the reply', async () => {
    const reply = {
      cluster_id: 'c1',
      total_nodes: 3,
      total_vectors: 100,
      total_namespaces: 2,
      total_storage_bytes: 4096,
      total_index_bytes: 1024,
      cluster_qps: 1.5,
      namespace_stats: [
        {
          namespace: 'docs',
          vector_count: 100,
          indexed_count: 100,
          storage_bytes: 4096,
          index_bytes: 1024,
          avg_search_latency_ms: 0.5,
          search_count: 7,
        },
      ],
      timestamp: '2026-11-12T00:00:00Z',
    };
    fetchMock.mockResolvedValue(make200JSON(reply));
    const client = new RestClient('http://localhost');
    const got = await client.vectors.clusterStats();
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/vector/cluster/stats');
    expect(init.method ?? 'GET').toBe('GET');
    expect(init.body).toBeUndefined();
    expect(got).toEqual(reply);
  });

  it('hybridSearch POSTs the request to /api/v1/vector/hybrid-search and returns the reply', async () => {
    const req: VectorHybridSearchRequest = {
      namespace: 'docs',
      query_vector: [0.1, 0.2, 0.3],
      k: 5,
      metadata_filters: { lang: 'en' },
      filter_operator: 'AND',
      include_metadata: true,
      min_score: 0.5,
    };
    const reply = {
      results: [{ id: 'v1', metadata: { lang: 'en' }, score: 0.9, rank: 1 }],
      total_candidates: 10,
      total_filtered: 1,
      search_time_ms: 2.5,
      node_id: 1,
    };
    fetchMock.mockResolvedValue(make200JSON(reply));
    const client = new RestClient('http://localhost');
    const got = await client.vectors.hybridSearch(req);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/vector/hybrid-search');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual(req);
    expect(got).toEqual(reply);
  });

  it('searchByMetadata POSTs the request to /api/v1/vector/search-by-metadata and returns the reply', async () => {
    const req: VectorMetadataSearchRequest = {
      namespace: 'docs',
      filters: { lang: 'en' },
      filter_operator: 'OR',
      limit: 10,
      offset: 0,
      include_vectors: false,
    };
    const reply = {
      results: [{ id: 'v1', metadata: { lang: 'en' }, version: 1, created_at: '2026-11-12T00:00:00Z' }],
      total_count: 1,
      next_cursor: 'abc',
    };
    fetchMock.mockResolvedValue(make200JSON(reply));
    const client = new RestClient('http://localhost');
    const got = await client.vectors.searchByMetadata(req);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/vector/search-by-metadata');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual(req);
    expect(got).toEqual(reply);
  });

  it('rejects with the status on a 403', async () => {
    fetchMock.mockResolvedValue(make403());
    const client = new RestClient('http://localhost');
    await expect(client.vectors.health()).rejects.toMatchObject({ status: 403 });
  });
});
