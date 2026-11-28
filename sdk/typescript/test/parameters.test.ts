// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// RestParametersClient: each method sends the route, HTTP method and body
// pkg/api/rest/handlers_parameters.go expects.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const BASE = 'http://localhost';
const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

function jsonResponse(status: number, payload: unknown) {
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

beforeEach(() => {
  fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { timestamp: '2026-11-12T00:00:00Z' }));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function lastCall(): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit | undefined];
  return {
    url: String(url),
    method: (init?.method ?? 'GET').toUpperCase(),
    body: init?.body === undefined ? undefined : JSON.parse(String(init.body)),
  };
}

describe('RestParametersClient', () => {
  it('get puts a nested path in the URL without its leading slash', async () => {
    const client = new RestClient(BASE);
    await client.parameters.get('/app/db/host');
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/app/db/host`, method: 'GET', body: undefined });
  });

  it('get encodes each segment and sends with_decryption only when given', async () => {
    const client = new RestClient(BASE);
    await client.parameters.get('/app/a b/c?d', { withDecryption: false });
    expect(lastCall().url).toBe(`${BASE}/api/v1/parameters/app/a%20b/c%3Fd?with_decryption=false`);
  });

  it('set PUTs the request body', async () => {
    const client = new RestClient(BASE);
    const req = { value: 'aGVsbG8=', type: 'String', tags: { env: 'dev' }, ttl_seconds: 60, overwrite: true };
    await client.parameters.set('/app/db/host', req);
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/app/db/host`, method: 'PUT', body: req });
  });

  it('delete sends DELETE and returns the JSON body', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { path: '/app/x', deleted: true, timestamp: 't' }));
    const client = new RestClient(BASE);
    const resp = await client.parameters.delete('/app/x');
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/app/x`, method: 'DELETE', body: undefined });
    expect(resp.deleted).toBe(true);
  });

  it('list with no options sends no query', async () => {
    const client = new RestClient(BASE);
    const resp = await client.parameters.list();
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/`, method: 'GET', body: undefined });
    expect(resp.parameters).toEqual([]);
  });

  it('list sends only the options given', async () => {
    const client = new RestClient(BASE);
    await client.parameters.list({ prefix: '/app', recursive: true, limit: 5 });
    const url = new URL(lastCall().url);
    expect(url.pathname).toBe('/api/v1/parameters/');
    expect(Object.fromEntries(url.searchParams)).toEqual({ prefix: '/app', recursive: 'true', limit: '5' });

    await client.parameters.list({ withValues: true, withDecryption: false, cursor: 'c1' });
    const url2 = new URL(lastCall().url);
    expect(Object.fromEntries(url2.searchParams)).toEqual({ with_values: 'true', with_decryption: 'false', cursor: 'c1' });
  });

  it('getBatch POSTs the paths', async () => {
    const client = new RestClient(BASE);
    const req = { paths: ['/a', '/b/c'], with_decryption: true };
    await client.parameters.getBatch(req);
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/batch/get`, method: 'POST', body: req });
  });

  it('setBatch POSTs the entries', async () => {
    const client = new RestClient(BASE);
    const req = { parameters: [{ path: '/a', value: 'eA==' }, { path: '/b', value: 'eQ==', audit_enabled: true }] };
    await client.parameters.setBatch(req);
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/batch/set`, method: 'POST', body: req });
  });

  it('deleteBatch POSTs {paths}', async () => {
    const client = new RestClient(BASE);
    await client.parameters.deleteBatch(['/a', '/b']);
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/batch/delete`, method: 'POST', body: { paths: ['/a', '/b'] } });
  });

  it('searchByTags POSTs the request', async () => {
    const client = new RestClient(BASE);
    const req = { tags: { env: 'prod' }, prefix: '/app', with_values: true, limit: 10 };
    await client.parameters.searchByTags(req);
    expect(lastCall()).toEqual({ url: `${BASE}/api/v1/parameters/search`, method: 'POST', body: req });
  });

  it('a 403 rejects', async () => {
    fetchMock.mockResolvedValue(jsonResponse(403, { error: 'forbidden' }));
    const client = new RestClient(BASE);
    await expect(client.parameters.get('/app/secret')).rejects.toThrow();
  });
});
