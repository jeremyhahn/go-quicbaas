// Tests for the RestIAMClient account link methods: each maps to one route
// under /api/v1/tenants/{tenant_id}/users/{user_id}/links, with every path
// segment and query value encoded.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

function makeJSON(body: unknown = {}, status = 200): Response {
  // fetchJSON reads a body even for 204 handlers, so the mock always has one.
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeError(status: number, message = 'error'): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
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

function lastCall(): [string, RequestInit | undefined] {
  return fetchMock.mock.calls[0] as [string, RequestInit | undefined];
}

const base = 'http://localhost/api/v1/tenants/t%2F1/users/u%2F1/links';

const link = {
  id: 'l1',
  user_id: '18446744073709551615',
  provider_id: 'p/1',
  provider_name: 'github',
  external_id: 'ext-1',
  created_at: '2026-11-12T00:00:00Z',
  updated_at: '2026-11-12T00:00:00Z',
};

describe('RestIAMClient account links', () => {
  it('createAccountLink POSTs links/ with the request body', async () => {
    fetchMock.mockResolvedValue(makeJSON(link, 201));
    const client = new RestClient('http://localhost');
    const req = { provider_id: 'p/1', external_id: 'ext-1', access_token: 'tok' };
    const resp = await client.iam.createAccountLink('t/1', 'u/1', req);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.user_id).toBe('18446744073709551615');
    expect(resp.provider_name).toBe('github');
  });

  it('listAccountLinks GETs links/ with no query and no body', async () => {
    fetchMock.mockResolvedValue(makeJSON({ links: [link], limit: 50, offset: 0 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listAccountLinks('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.links).toHaveLength(1);
    expect(resp.links[0].external_id).toBe('ext-1');
  });

  it('listAccountLinks sends limit and offset', async () => {
    fetchMock.mockResolvedValue(makeJSON({ links: [], limit: 10, offset: 20 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listAccountLinks('t/1', 'u/1', { limit: 10, offset: 20 });

    const [url] = lastCall();
    expect(url).toBe(`${base}/?limit=10&offset=20`);
    expect(resp.limit).toBe(10);
    expect(resp.offset).toBe(20);
  });

  it('deleteAccountLink DELETEs links/{provider_id} with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.deleteAccountLink('t/1', 'u/1', 'p/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/p%2F1`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });

  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.listAccountLinks('t1', 'someone-else')).rejects.toBeInstanceOf(APIError);
  });
});
