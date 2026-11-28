// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Wire-format tests for the IAM session methods of RestIAMClient: each one
// must hit the /api/v1/iam/sessions route the REST server mounts, with the
// HTTP method, query string and body the Go handlers read.

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

describe('RestIAMClient.listSessions', () => {
  it('GETs /api/v1/iam/sessions with no query string by default', async () => {
    const session = {
      id: 's1',
      created: '2026-11-12T00:00:00Z',
      last_active: '2026-11-12T00:01:00Z',
      expires_at: '2026-11-13T00:00:00Z',
      mfa_verified: true,
      ip_address: '10.0.0.1',
      user_agent: 'ua',
      device_info: null,
    };
    fetchMock.mockResolvedValue(makeJSON({ sessions: [session] }));

    const client = new RestClient('http://localhost');
    const resp = await client.iam.listSessions();

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.sessions).toEqual([session]);
  });

  it('sends user_id and tenant_id as query parameters', async () => {
    fetchMock.mockResolvedValue(makeJSON({ sessions: [] }));

    const client = new RestClient('http://localhost');
    await client.iam.listSessions({ user_id: 'u 1', tenant_id: 't1' });

    const [url] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions?user_id=u+1&tenant_id=t1');
  });
});

describe('RestIAMClient.getSession', () => {
  it('GETs the encoded session ID', async () => {
    fetchMock.mockResolvedValue(makeJSON({ id: 'a/b' }));

    const client = new RestClient('http://localhost');
    const s = await client.iam.getSession('a/b');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions/a%2Fb');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(s.id).toBe('a/b');
  });
});

describe('RestIAMClient.revokeSession', () => {
  it('DELETEs the encoded session ID with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON());

    const client = new RestClient('http://localhost');
    await client.iam.revokeSession('a/b');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions/a%2Fb');
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient.revokeAllSessions', () => {
  it('DELETEs /api/v1/iam/sessions with no query string by default', async () => {
    fetchMock.mockResolvedValue(makeJSON());

    const client = new RestClient('http://localhost');
    await client.iam.revokeAllSessions();

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions');
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });

  it('sends except as an encoded query parameter', async () => {
    fetchMock.mockResolvedValue(makeJSON());

    const client = new RestClient('http://localhost');
    await client.iam.revokeAllSessions({ except: 'a/b' });

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions?except=a%2Fb');
    expect(init?.method).toBe('DELETE');
  });
});

describe('RestIAMClient.revokeOtherSessions', () => {
  it('POSTs { keep_session_id } to revoke-others', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({ message: 'other sessions revoked successfully', kept_session_id: 'a/b' }),
    );

    const client = new RestClient('http://localhost');
    const resp = await client.iam.revokeOtherSessions('a/b');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/iam/sessions/revoke-others');
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ keep_session_id: 'a/b' });
    expect(resp.kept_session_id).toBe('a/b');
  });
});

describe('RestIAMClient session errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));

    const client = new RestClient('http://localhost');
    await expect(client.iam.getSession('someone-else')).rejects.toBeInstanceOf(APIError);
  });
});
