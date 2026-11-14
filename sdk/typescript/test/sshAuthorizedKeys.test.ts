// Tests for the stored authorized_keys methods of RestAuthorizedKeysClient:
// add, list, get and remove over /api/v1/ssh/authorized/keys*.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestAuthorizedKeysClient } from '../src/rest';

const entryFixture = {
  scope: 'user',
  owner: 'alice',
  name: 'laptop',
  line: 'ssh-ed25519 AAA alice@quicbaas',
  fingerprint: 'SHA256:abc',
};

function jsonResp(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function emptyResp(status: number): Response {
  return new Response(status === 204 ? null : '', { status });
}

describe('RestAuthorizedKeysClient stored entries', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function call(i = 0): [string, RequestInit] {
    return fetchMock.mock.calls[i] as [string, RequestInit];
  }

  it('add POSTs the request and resolves on a 201 with an empty body', async () => {
    fetchMock.mockResolvedValue(emptyResp(201));
    const client = new RestAuthorizedKeysClient('http://localhost');
    const req = {
      scope: 'user' as const,
      owner: 'alice',
      name: 'laptop',
      line: 'ssh-ed25519 AAA',
      options: ['restrict'],
      comment: 'alice@quicbaas',
    };
    await expect(client.add(req)).resolves.toBeUndefined();
    const [url, init] = call();
    expect(url).toBe('http://localhost/api/v1/ssh/authorized/keys');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual(req);
  });

  it('list GETs without a query and returns items', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [entryFixture] }));
    const client = new RestAuthorizedKeysClient('http://localhost');
    const items = await client.list();
    expect(items).toHaveLength(1);
    expect(items[0].name).toBe('laptop');
    const [url, init] = call();
    expect(url).toBe('http://localhost/api/v1/ssh/authorized/keys');
    expect(init.method).toBe('GET');
    expect(init.body).toBeUndefined();
  });

  it('list encodes scope and owner in the query', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [] }));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await client.list({ scope: 'service', owner: 'ci/runner' });
    expect(call()[0]).toBe(
      'http://localhost/api/v1/ssh/authorized/keys?scope=service&owner=ci%2Frunner',
    );
  });

  it('list omits an empty query value', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [] }));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await client.list({ scope: 'user', owner: '' });
    expect(call()[0]).toBe('http://localhost/api/v1/ssh/authorized/keys?scope=user');
  });

  it('list returns [] when items is null or absent', async () => {
    const client = new RestAuthorizedKeysClient('http://localhost');
    fetchMock.mockResolvedValueOnce(jsonResp({ items: null }));
    expect(await client.list()).toEqual([]);
    fetchMock.mockResolvedValueOnce(jsonResp({}));
    expect(await client.list()).toEqual([]);
  });

  it('get GETs the encoded entry path', async () => {
    fetchMock.mockResolvedValue(jsonResp(entryFixture));
    const client = new RestAuthorizedKeysClient('http://localhost');
    const entry = await client.get('user', 'team/alice', 'my laptop');
    expect(entry.line).toContain('ssh-ed25519');
    const [url, init] = call();
    expect(url).toBe(
      'http://localhost/api/v1/ssh/authorized/keys/user/team%2Falice/my%20laptop',
    );
    expect(init.method).toBe('GET');
    expect(init.body).toBeUndefined();
  });

  it('remove DELETEs the encoded entry path and resolves on a 204', async () => {
    fetchMock.mockResolvedValue(emptyResp(204));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await expect(client.remove('tenant', 'acme/ops', 'deploy')).resolves.toBeUndefined();
    const [url, init] = call();
    expect(url).toBe('http://localhost/api/v1/ssh/authorized/keys/tenant/acme%2Fops/deploy');
    expect(init.method).toBe('DELETE');
    expect(init.body).toBeUndefined();
  });

  it('rejects on a 403', async () => {
    fetchMock.mockResolvedValue(jsonResp({ error: 'forbidden' }, 403));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await expect(client.list()).rejects.toThrow();
  });
});
