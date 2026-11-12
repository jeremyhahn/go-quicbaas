// Tests for the RestIAMClient delegated-admin impersonation methods: URL,
// HTTP method and body of each call, the list query, and the end result's
// two shapes.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

function makeJSON(body: unknown = {}): Response {
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

const session = {
  id: 's1',
  admin_user_id: '9007199254740993',
  target_user_id: '42',
  tenant_id: 't1',
  reason: 'support ticket',
  started_at: '2026-11-12T10:00:00Z',
  expires_at: '2026-11-12T10:30:00Z',
  operation_count: 0,
  audit_log_id: 'a1',
};

const base = 'http://localhost/api/v1/admin/impersonation';

describe('RestIAMClient listImpersonations', () => {
  it('GETs the collection with no query when no options are set', async () => {
    fetchMock.mockResolvedValue(makeJSON({ sessions: [session], total: 1 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listImpersonations();

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.total).toBe(1);
    expect(resp.sessions[0].admin_user_id).toBe('9007199254740993');
  });

  it('sends active=true and the encoded admin_user_id when set', async () => {
    fetchMock.mockResolvedValue(makeJSON({ sessions: [], total: 0 }));
    const client = new RestClient('http://localhost');
    await client.iam.listImpersonations({ activeOnly: true, adminUserID: 'a/1' });

    const [url] = lastCall();
    expect(url).toBe(`${base}/?active=true&admin_user_id=a%2F1`);
  });

  it('omits active when activeOnly is false', async () => {
    fetchMock.mockResolvedValue(makeJSON({ sessions: [], total: 0 }));
    const client = new RestClient('http://localhost');
    await client.iam.listImpersonations({ activeOnly: false, adminUserID: '7' });

    const [url] = lastCall();
    expect(url).toBe(`${base}/?admin_user_id=7`);
  });
});

describe('RestIAMClient startImpersonation', () => {
  it('POSTs the request to start', async () => {
    fetchMock.mockResolvedValue(makeJSON(session));
    const client = new RestClient('http://localhost');
    const req = { target_user_id: '42', tenant_id: 't1', reason: 'support ticket', duration: '30m' };
    const resp = await client.iam.startImpersonation(req);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/start`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.id).toBe('s1');
  });
});

describe('RestIAMClient endImpersonation', () => {
  it('POSTs to the encoded session end and returns the session', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...session, ended_at: '2026-11-12T10:05:00Z' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.endImpersonation('s/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/s%2F1/end`);
    expect(init?.method).toBe('POST');
    expect(init?.body).toBeUndefined();
    expect('id' in resp && resp.ended_at).toBe('2026-11-12T10:05:00Z');
  });

  it('returns the bare status when the server cannot reread the session', async () => {
    fetchMock.mockResolvedValue(makeJSON({ status: 'impersonation_ended' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.endImpersonation('s1');

    expect(resp).toEqual({ status: 'impersonation_ended' });
  });
});

describe('RestIAMClient listUserImpersonations', () => {
  it('GETs the encoded user impersonations', async () => {
    fetchMock.mockResolvedValue(makeJSON({ user_id: '42', sessions: [session] }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listUserImpersonations('u/42');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/admin/users/u%2F42/impersonations');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.user_id).toBe('42');
    expect(resp.sessions).toHaveLength(1);
  });
});

describe('RestIAMClient impersonation errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(
      client.iam.startImpersonation({ target_user_id: '42', tenant_id: 't1', reason: 'r' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});
