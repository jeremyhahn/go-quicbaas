// Tests for RestIAMClient's federation provider methods and the single-role read.

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

const tenantBase = 'http://localhost/api/v1/tenants/t%2F1';

const provider = {
  id: 'p/1',
  tenant_id: 't/1',
  name: 'Corp',
  type: 'oidc',
  enabled: true,
  client_id: 'cid',
  client_secret: '',
  issuer_url: 'https://idp.example',
  scopes: ['openid'],
  claim_mappings: { email: 'mail' },
  auto_provision: false,
  allow_linking: true,
  created: '2026-11-12T00:00:00Z',
  updated: '2026-11-12T00:00:00Z',
};

describe('RestIAMClient getRole', () => {
  it('GETs the encoded role ID with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON({ id: 'r/1', name: 'admin' }));
    const client = new RestClient('http://localhost');
    const role = await client.iam.getRole('t/1', 'r/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${tenantBase}/roles/r%2F1`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(role.name).toBe('admin');
  });
});

describe('RestIAMClient providers', () => {
  it('registerProvider POSTs the request to providers/', async () => {
    fetchMock.mockResolvedValue(makeJSON(provider, 201));
    const client = new RestClient('http://localhost');
    const req = {
      name: 'Corp',
      type: 'oidc' as const,
      enabled: true,
      client_id: 'cid',
      client_secret: 'sec',
      issuer: 'https://idp.example',
      claim_mapping: { email: 'mail' },
    };
    const resp = await client.iam.registerProvider('t/1', req);

    const [url, init] = lastCall();
    expect(url).toBe(`${tenantBase}/providers/`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.issuer_url).toBe('https://idp.example');
    expect(resp.client_secret).toBe('');
  });

  it('listProviders GETs providers/ with no query by default', async () => {
    fetchMock.mockResolvedValue(makeJSON({ providers: [provider], limit: 50, offset: 0 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listProviders('t/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${tenantBase}/providers/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.providers).toHaveLength(1);
    expect(resp.limit).toBe(50);
  });

  it('listProviders sends limit, offset and type', async () => {
    fetchMock.mockResolvedValue(makeJSON({ providers: [], limit: 10, offset: 20 }));
    const client = new RestClient('http://localhost');
    await client.iam.listProviders('t/1', { limit: 10, offset: 20, type: 'saml' });

    const [url] = lastCall();
    expect(url).toBe(`${tenantBase}/providers/?limit=10&offset=20&type=saml`);
  });

  it('getProvider GETs the encoded provider ID', async () => {
    fetchMock.mockResolvedValue(makeJSON(provider));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.getProvider('t/1', 'p/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${tenantBase}/providers/p%2F1`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.id).toBe('p/1');
  });

  it('updateProvider PUTs only the given fields', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...provider, enabled: false }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.updateProvider('t/1', 'p/1', { enabled: false, scopes: ['openid', 'email'] });

    const [url, init] = lastCall();
    expect(url).toBe(`${tenantBase}/providers/p%2F1`);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(init?.body as string)).toEqual({ enabled: false, scopes: ['openid', 'email'] });
    expect(resp.enabled).toBe(false);
  });

  it('deleteProvider DELETEs the encoded provider ID with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.deleteProvider('t/1', 'p/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${tenantBase}/providers/p%2F1`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient provider errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.listProviders('t1')).rejects.toBeInstanceOf(APIError);
  });
});
