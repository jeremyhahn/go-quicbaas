// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestRegistryClient (client.registry): each method's URL, HTTP
// method and body, the 201 register reply, lease_id staying a string, and a
// 403 rejecting.

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

type FakeResponse = {
  ok: boolean;
  status: number;
  statusText: string;
  headers: Headers;
  json: () => Promise<unknown>;
  text: () => Promise<string>;
};

function reply(status: number, payload?: unknown): FakeResponse {
  const body = payload === undefined ? '' : JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

function call(i = 0): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[i] as [string | URL, RequestInit | undefined];
  return {
    url: String(url),
    method: (init?.method ?? 'GET').toUpperCase(),
    body: init?.body === undefined || init?.body === null ? undefined : JSON.parse(String(init.body)),
  };
}

describe('RestRegistryClient', () => {
  it('register POSTs the instance and ttl and returns the 201 lease', async () => {
    fetchMock.mockResolvedValueOnce(reply(201, { lease_id: '9223372036854775807', ttl_seconds: 30 }));
    const client = new RestClient('http://localhost');
    const instance = { id: 'i-1', service: 'api', address: '10.0.0.1:443', metadata: { zone: 'a' } };
    const lease = await client.registry.register(instance, 30);
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/registry/instances');
    expect(c.method).toBe('POST');
    expect(c.body).toEqual({ instance, ttl_seconds: 30 });
    expect(lease.lease_id).toBe('9223372036854775807');
    expect(typeof lease.lease_id).toBe('string');
    expect(lease.ttl_seconds).toBe(30);
  });

  it('renew POSTs to the encoded lease path with no body', async () => {
    fetchMock.mockResolvedValueOnce(reply(200, { lease_id: '42', ttl_seconds: 15 }));
    const client = new RestClient('http://localhost');
    const lease = await client.registry.renew('4/2');
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/registry/leases/4%2F2/renew');
    expect(c.method).toBe('POST');
    expect(c.body).toBeUndefined();
    expect(lease.lease_id).toBe('42');
    expect(typeof lease.lease_id).toBe('string');
  });

  it('deregister DELETEs the encoded lease path and resolves on 204', async () => {
    fetchMock.mockResolvedValueOnce(reply(204));
    const client = new RestClient('http://localhost');
    await expect(client.registry.deregister('7/8')).resolves.toBeUndefined();
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/registry/leases/7%2F8');
    expect(c.method).toBe('DELETE');
    expect(c.body).toBeUndefined();
  });

  it('resolve GETs the encoded service path', async () => {
    const instances = [{ id: 'i-1', service: 'team/api', address: '10.0.0.1:443' }];
    fetchMock.mockResolvedValueOnce(reply(200, { instances, revision: 5 }));
    const client = new RestClient('http://localhost');
    const res = await client.registry.resolve('team/api');
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/registry/services/team%2Fapi');
    expect(c.method).toBe('GET');
    expect(c.body).toBeUndefined();
    expect(res).toEqual({ instances, revision: 5 });
  });

  it('resolve maps a null instances list to an empty array', async () => {
    fetchMock.mockResolvedValueOnce(reply(200, { instances: null, revision: 0 }));
    const client = new RestClient('http://localhost');
    const res = await client.registry.resolve('api');
    expect(res.instances).toEqual([]);
  });

  it('a 403 rejects', async () => {
    fetchMock.mockResolvedValueOnce(reply(403, { error: 'forbidden' }));
    const client = new RestClient('http://localhost');
    await expect(client.registry.resolve('api')).rejects.toThrow();
  });
});
