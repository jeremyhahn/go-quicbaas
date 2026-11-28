// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// RestCacheClient: each method sends the documented route, HTTP method and
// body, and a 403 rejects.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const BASE = 'http://localhost:8080';
const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function lastCall(): { url: string; init: RequestInit } {
  const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
  return { url: String(call[0]), init: (call[1] ?? {}) as RequestInit };
}

function method(init: RequestInit): string {
  return (init.method ?? 'GET').toUpperCase();
}

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe('RestCacheClient', () => {
  it('is exposed as client.cache', () => {
    const client = new RestClient(BASE);
    expect(client.cache).toBeDefined();
  });

  it('stats sends GET /api/v1/cache/stats with no body', async () => {
    const stats = {
      enabled: true,
      l1_size: 3,
      l1_capacity: 100,
      l1_hits: 7,
      l1_misses: 1,
      l1_hit_rate: 0.875,
      l1_evictions: 0,
    };
    fetchMock.mockResolvedValueOnce(jsonResponse(stats));
    const got = await new RestClient(BASE).cache.stats();
    const { url, init } = lastCall();
    expect(url).toBe(`${BASE}/api/v1/cache/stats`);
    expect(method(init)).toBe('GET');
    expect(init.body).toBeUndefined();
    expect(got).toEqual(stats);
  });

  it('clear sends POST /api/v1/cache/clear with no body', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, entries_cleared: 4 }));
    const got = await new RestClient(BASE).cache.clear();
    const { url, init } = lastCall();
    expect(url).toBe(`${BASE}/api/v1/cache/clear`);
    expect(method(init)).toBe('POST');
    expect(init.body).toBeUndefined();
    expect(got).toEqual({ success: true, entries_cleared: 4 });
  });

  it('getConfig sends GET /api/v1/cache/config with no body', async () => {
    const cfg = { enabled: true, l1_size: 100, l1_ttl_seconds: 60, cleanup_interval_seconds: 30 };
    fetchMock.mockResolvedValueOnce(jsonResponse(cfg));
    const got = await new RestClient(BASE).cache.getConfig();
    const { url, init } = lastCall();
    expect(url).toBe(`${BASE}/api/v1/cache/config`);
    expect(method(init)).toBe('GET');
    expect(init.body).toBeUndefined();
    expect(got).toEqual(cfg);
  });

  it('updateConfig sends PUT /api/v1/cache/config with both fields', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, message: 'updated' }));
    const got = await new RestClient(BASE).cache.updateConfig({ l1_size: 200, l1_ttl_seconds: 90 });
    const { url, init } = lastCall();
    expect(url).toBe(`${BASE}/api/v1/cache/config`);
    expect(method(init)).toBe('PUT');
    expect(JSON.parse(String(init.body))).toEqual({ l1_size: 200, l1_ttl_seconds: 90 });
    expect(got).toEqual({ success: true, message: 'updated' });
  });

  it('updateConfig sends only the fields given', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, message: 'updated' }));
    await new RestClient(BASE).cache.updateConfig({ l1_ttl_seconds: 15 });
    const { init } = lastCall();
    expect(JSON.parse(String(init.body))).toEqual({ l1_ttl_seconds: 15 });

    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, message: 'updated' }));
    await new RestClient(BASE).cache.updateConfig({ l1_size: 0, l1_ttl_seconds: undefined });
    expect(JSON.parse(String(lastCall().init.body))).toEqual({ l1_size: 0 });
  });

  it('a 403 rejects', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'forbidden' }, 403));
    await expect(new RestClient(BASE).cache.clear()).rejects.toThrow();
  });
});
