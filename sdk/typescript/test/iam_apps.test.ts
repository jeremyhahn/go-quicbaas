// Tests for the IAM OAuth application methods of RestIAMClient: each asserts
// the URL (with encoded path segments), the HTTP method and the body.
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

const base = 'http://localhost/api/v1/tenants/t%2F1/apps';

const app = {
  client_id: 'c/1',
  name: 'web',
  tenant_id: 't/1',
  redirect_uris: ['https://app.example/cb'],
  grant_types: ['authorization_code'],
  response_types: ['code'],
  created: '2026-11-12T00:00:00Z',
  updated: '2026-11-12T00:00:00Z',
};

describe('RestIAMClient OAuth applications', () => {
  it('listApps GETs apps/ with no query by default', async () => {
    fetchMock.mockResolvedValue(makeJSON({ applications: [app], apps: [app], limit: 50, offset: 0 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listApps('t/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.applications[0].client_id).toBe('c/1');
    expect(resp.apps).toHaveLength(1);
  });

  it('listApps sends limit and offset', async () => {
    fetchMock.mockResolvedValue(makeJSON({ applications: [], apps: [], limit: 10, offset: 20 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listApps('t/1', { limit: 10, offset: 20 });

    const [url] = lastCall();
    expect(url).toBe(`${base}/?limit=10&offset=20`);
    expect(resp.offset).toBe(20);
  });

  it('registerApp POSTs the request to apps/', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...app, client_secret: 's3cret' }, 201));
    const client = new RestClient('http://localhost');
    const req = {
      name: 'web',
      redirect_uris: ['https://app.example/cb'],
      grant_types: ['authorization_code'],
      pkce_required: true,
    };
    const resp = await client.iam.registerApp('t/1', req);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.client_secret).toBe('s3cret');
  });

  it('getApp GETs the encoded client ID', async () => {
    fetchMock.mockResolvedValue(makeJSON(app));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.getApp('t/1', 'c/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/c%2F1`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.name).toBe('web');
  });

  it('updateApp PUTs the request with client_id filled in', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...app, name: 'renamed' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.updateApp('t/1', 'c/1', { name: 'renamed', scopes: ['read'] });

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/c%2F1`);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(init?.body as string)).toEqual({ client_id: 'c/1', name: 'renamed', scopes: ['read'] });
    expect(resp.name).toBe('renamed');
  });

  it('deleteApp DELETEs the encoded client ID with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.deleteApp('t/1', 'c/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/c%2F1`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });

  it('rotateAppSecret POSTs to rotate-secret with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...app, client_secret: 'new' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.rotateAppSecret('t/1', 'c/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/c%2F1/rotate-secret`);
    expect(init?.method).toBe('POST');
    expect(init?.body).toBeUndefined();
    expect(resp.client_secret).toBe('new');
  });
});

describe('RestIAMClient OAuth application errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.getApp('t1', 'c1')).rejects.toBeInstanceOf(APIError);
  });
});
