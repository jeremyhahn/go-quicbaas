// Tests for the RestIAMClient WebAuthn credential and identity-link methods:
// each maps to one route under /api/v1/tenants/{tenant_id}/users/{user_id},
// with every path segment encoded.

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

const base = 'http://localhost/api/v1/tenants/t%2F1/users/u%2F1';

describe('RestIAMClient WebAuthn credentials', () => {
  it('listWebAuthnCredentials GETs webauthn/credentials/ with no body', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({
        credentials: [
          { id: 'c1', sign_count: 3, backup_eligible: true, backup_state: false, clone_warning: false },
        ],
        total: 1,
      }),
    );
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listWebAuthnCredentials('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/webauthn/credentials/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.total).toBe(1);
    expect(resp.credentials[0].sign_count).toBe(3);
  });

  it('deleteAllWebAuthnCredentials DELETEs webauthn/credentials/ and returns the count', async () => {
    fetchMock.mockResolvedValue(makeJSON({ deleted: 2 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.deleteAllWebAuthnCredentials('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/webauthn/credentials/`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
    expect(resp.deleted).toBe(2);
  });

  it('deleteWebAuthnCredential DELETEs the encoded credential ID', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.deleteWebAuthnCredential('t/1', 'u/1', 'c/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/webauthn/credentials/c%2F1`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient identity links', () => {
  it('createAccountLink POSTs the request to links/', async () => {
    fetchMock.mockResolvedValue(
      makeJSON(
        {
          id: 'l1',
          user_id: 'u/1',
          provider_id: 'p1',
          provider_name: 'GitHub',
          external_id: 'x1',
          created_at: 'z',
          updated_at: 'z',
        },
        201,
      ),
    );
    const client = new RestClient('http://localhost');
    const resp = await client.iam.createAccountLink('t/1', 'u/1', {
      provider_id: 'p1',
      external_id: 'x1',
      access_token: 'tok',
    });

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/links/`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ provider_id: 'p1', external_id: 'x1', access_token: 'tok' });
    expect(resp.provider_name).toBe('GitHub');
  });

  it('listAccountLinks GETs links/ with no query when no options are given', async () => {
    fetchMock.mockResolvedValue(makeJSON({ links: [], limit: 50, offset: 0 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listAccountLinks('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/links/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.limit).toBe(50);
  });

  it('listAccountLinks sends limit and offset as the query', async () => {
    fetchMock.mockResolvedValue(makeJSON({ links: [], limit: 10, offset: 20 }));
    const client = new RestClient('http://localhost');
    await client.iam.listAccountLinks('t/1', 'u/1', { limit: 10, offset: 20 });

    const [url] = lastCall();
    expect(url).toBe(`${base}/links/?limit=10&offset=20`);
  });

  it('deleteAccountLink DELETEs the encoded provider ID', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.deleteAccountLink('t/1', 'u/1', 'p/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/links/p%2F1`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient credential and link errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.listAccountLinks('t1', 'someone-else')).rejects.toBeInstanceOf(APIError);
  });
});
