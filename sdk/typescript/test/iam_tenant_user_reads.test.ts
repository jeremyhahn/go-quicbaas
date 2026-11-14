// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Wire-format tests for the admin tenant/user read methods of RestIAMClient:
// getTenant, getUser, listUserSessions and revokeAllUserSessions must hit the
// /api/v1/tenants routes with encoded path segments and the right HTTP method.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

function makeJSON(body: unknown = {}): Response {
  // fetchJSON reads a body even for 204 handlers, so the mock always has one.
  return new Response(JSON.stringify(body), {
    status: 200,
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

describe('RestIAMClient.getTenant', () => {
  it('GETs the encoded tenant ID and maps the wire fields', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({ id: 't/1', name: 'Acme', created: '2026-11-12T00:00:00Z', updated: '2026-11-12T00:01:00Z' }),
    );

    const client = new RestClient('http://localhost');
    const t = await client.iam.getTenant('t/1');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/t%2F1');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(t).toEqual({
      id: 't/1',
      name: 'Acme',
      description: '',
      created_at: '2026-11-12T00:00:00Z',
      updated_at: '2026-11-12T00:01:00Z',
    });
  });
});

describe('RestIAMClient.getUser', () => {
  it('GETs the encoded tenant and user IDs and maps the wire fields', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({ id: 'u/1', email: 'a@b.c', name: 'Ann', tenant_id: 't/1', role: 'admin', created: 'c' }),
    );

    const client = new RestClient('http://localhost');
    const u = await client.iam.getUser('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/t%2F1/users/u%2F1');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(u.id).toBe('u/1');
    expect(u.username).toBe('Ann');
    expect(u.roles).toEqual(['admin']);
    expect(u.created_at).toBe('c');
  });
});

describe('RestIAMClient.listUserSessions', () => {
  it('GETs the user sessions with no query string by default', async () => {
    const session = { id: 's1', created: 'c', last_active: 'l', expires_at: 'e', mfa_verified: false };
    fetchMock.mockResolvedValue(makeJSON({ sessions: [session] }));

    const client = new RestClient('http://localhost');
    const resp = await client.iam.listUserSessions('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/t%2F1/users/u%2F1/sessions');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.sessions).toEqual([session]);
  });

  it('sends limit only when it is greater than zero', async () => {
    fetchMock.mockImplementation(async () => makeJSON({ sessions: [] }));
    const client = new RestClient('http://localhost');

    await client.iam.listUserSessions('t1', 'u1', 5);
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/tenants/t1/users/u1/sessions?limit=5');

    await client.iam.listUserSessions('t1', 'u1', 0);
    expect(fetchMock.mock.calls[1][0]).toBe('http://localhost/api/v1/tenants/t1/users/u1/sessions');
  });
});

describe('RestIAMClient.revokeAllUserSessions', () => {
  it('DELETEs the user sessions with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON());

    const client = new RestClient('http://localhost');
    await client.iam.revokeAllUserSessions('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/t%2F1/users/u%2F1/sessions');
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient tenant/user read errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));

    const client = new RestClient('http://localhost');
    await expect(client.iam.getUser('t1', 'u1')).rejects.toBeInstanceOf(APIError);
  });
});
