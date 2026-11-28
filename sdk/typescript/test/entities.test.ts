// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestEntitiesClient: each method's URL, HTTP method and body
// against the generic entity routes (/api/v1/entities/*).

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

function makeJSON(status: number, payload: unknown) {
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
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit | undefined];
  const rawBody = init?.body;
  return {
    url: String(url),
    method: init?.method ?? 'GET',
    body: typeof rawBody === 'string' ? JSON.parse(rawBody) : undefined,
  };
}

const BASE = 'http://localhost';
const TS = '2026-11-12T00:00:00Z';

describe('entities.list', () => {
  it('GETs the collection with a trailing slash and no query when no option is set', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { entities: [{ id: 1, entity_type: 'a/b', data: {} }], has_more: false }));
    const res = await new RestClient(BASE).entities.list('a/b');
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/entities/a%2Fb/`);
    expect(c.method).toBe('GET');
    expect(c.body).toBeUndefined();
    expect(res.entities).toHaveLength(1);
  });

  it('sends each query parameter only when set', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { entities: [], has_more: true, next_cursor: 'n' }));
    const client = new RestClient(BASE);
    await client.entities.list('user', { limit: 10, cursor: 'c&1', consistency: 'linearizable' });
    expect(lastCall().url).toBe(`${BASE}/api/v1/entities/user/?limit=10&cursor=c%261&consistency=linearizable`);
    await client.entities.list('user', { cursor: 'x' });
    expect(lastCall().url).toBe(`${BASE}/api/v1/entities/user/?cursor=x`);
  });

  it('defaults a missing entities array to empty', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { has_more: false }));
    const res = await new RestClient(BASE).entities.list('user');
    expect(res.entities).toEqual([]);
  });
});

describe('entities.create', () => {
  it('is withheld until the server assigns ids on POST', () => {
    const entities = new RestClient(BASE).entities as unknown as Record<string, unknown>;
    expect('create' in entities).toBe(false);
    expect(entities.create).toBeUndefined();
  });
});

describe('entities.put', () => {
  it('PUTs to the item with the exact id in the URL only', async () => {
    // HandlePut decodes the body into PutRequest, whose id is a uint64: a
    // quoted id there is a 400. The URL id is the one the handler uses.
    fetchMock.mockResolvedValue(makeJSON(200, { id: 42, entity_type: 'a/b', created: false, timestamp: TS }));
    await new RestClient(BASE).entities.put('a/b', '18446744073709551615', [1, 2]);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/entities/a%2Fb/18446744073709551615`);
    expect(c.method).toBe('PUT');
    expect(c.body).toEqual({ entity_type: 'a/b', data: [1, 2] });
    expect(c.body).not.toHaveProperty('id');
  });

  it('keeps a numeric id out of the body too', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { id: 7, entity_type: 'users', created: true, timestamp: TS }));
    await new RestClient(BASE).entities.put('users', 7, { name: 'a' });
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/entities/users/7`);
    expect(c.body).toEqual({ entity_type: 'users', data: { name: 'a' } });
  });

  it('sends write_mode only when set', async () => {
    fetchMock.mockResolvedValue(makeJSON(201, { id: 42, entity_type: 'user', created: true, timestamp: TS }));
    await new RestClient(BASE).entities.put('user', 42, {}, { write_mode: 'async' });
    expect(lastCall().url).toBe(`${BASE}/api/v1/entities/user/42?write_mode=async`);
  });
});

describe('entities.get', () => {
  it('GETs the item with no query when no option is set', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { id: 42, entity_type: 'a/b', data: { k: 'v' }, version: 3 }));
    const rec = await new RestClient(BASE).entities.get('a/b', 42);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/entities/a%2Fb/42`);
    expect(c.method).toBe('GET');
    expect(c.body).toBeUndefined();
    expect(rec.data).toEqual({ k: 'v' });
  });

  it('sends each query parameter only when set', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { id: 42, entity_type: 'user', data: null }));
    const client = new RestClient(BASE);
    await client.entities.get('user', 42, { consistency: 'stale', timeout_ms: 500 });
    expect(lastCall().url).toBe(`${BASE}/api/v1/entities/user/42?consistency=stale&timeout_ms=500`);
    await client.entities.get('user', 42, { timeout_ms: 500 });
    expect(lastCall().url).toBe(`${BASE}/api/v1/entities/user/42?timeout_ms=500`);
  });
});

describe('entities.delete', () => {
  it('DELETEs the item and returns the JSON body', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { id: 42, entity_type: 'a/b', deleted: true, timestamp: TS }));
    const res = await new RestClient(BASE).entities.delete('a/b', 42);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/entities/a%2Fb/42`);
    expect(c.method).toBe('DELETE');
    expect(c.body).toBeUndefined();
    expect(res.deleted).toBe(true);
  });

  it('sends write_mode only when set', async () => {
    fetchMock.mockResolvedValue(makeJSON(200, { id: 42, entity_type: 'user', deleted: true, timestamp: TS }));
    await new RestClient(BASE).entities.delete('user', 42, { write_mode: 'sync' });
    expect(lastCall().url).toBe(`${BASE}/api/v1/entities/user/42?write_mode=sync`);
  });
});

describe('entities errors', () => {
  it('rejects on a 403', async () => {
    fetchMock.mockResolvedValue(makeJSON(403, { error: 'forbidden' }));
    await expect(new RestClient(BASE).entities.get('user', 1)).rejects.toThrow();
  });
});
