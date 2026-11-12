// Tests for the RestIAMClient MFA methods: each maps to one route under
// /api/v1/tenants/{tenant_id}/users/{user_id}/mfa, with every path segment
// encoded.

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

const base = 'http://localhost/api/v1/tenants/t%2F1/users/u%2F1/mfa';

describe('RestIAMClient MFA status', () => {
  it('getMFAStatus GETs mfa/ with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON({ enabled: true, method: 'totp', backup_codes_remaining: 8 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.getMFAStatus('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.enabled).toBe(true);
    expect(resp.backup_codes_remaining).toBe(8);
  });

  it('disableMFA DELETEs mfa/ with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.disableMFA('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient MFA enrollment', () => {
  it('beginMFAEnrollment POSTs { method } to enroll', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({ enrollment_id: 'e1', provisioning_uri: 'otpauth://x', secret: 's', expires_at: 'z' }),
    );
    const client = new RestClient('http://localhost');
    const resp = await client.iam.beginMFAEnrollment('t/1', 'u/1', 'totp');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/enroll`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ method: 'totp' });
    expect(resp.enrollment_id).toBe('e1');
    expect(resp.provisioning_uri).toBe('otpauth://x');
  });

  it('beginMFAEnrollment sends an empty object when no method is given', async () => {
    fetchMock.mockResolvedValue(makeJSON({}));
    const client = new RestClient('http://localhost');
    await client.iam.beginMFAEnrollment('t/1', 'u/1');

    const [, init] = lastCall();
    expect(JSON.parse(init?.body as string)).toEqual({});
  });

  it('completeMFAEnrollment POSTs { enrollment_id, code } to verify', async () => {
    fetchMock.mockResolvedValue(makeJSON({ success: true }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.completeMFAEnrollment('t/1', 'u/1', 'e1', '123456');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/verify`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ enrollment_id: 'e1', code: '123456' });
    expect(resp.success).toBe(true);
  });

  it('cancelMFAEnrollment DELETEs the encoded enrollment ID', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const client = new RestClient('http://localhost');
    await client.iam.cancelMFAEnrollment('t/1', 'u/1', 'e/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/enroll/e%2F1`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
  });
});

describe('RestIAMClient MFA challenge', () => {
  it('beginMFAChallenge POSTs { session_token } to challenge', async () => {
    fetchMock.mockResolvedValue(makeJSON({ challenge_id: 'c1', method: 'totp', expires_at: 'z' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.beginMFAChallenge('t/1', 'u/1', 'tok');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/challenge`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ session_token: 'tok' });
    expect(resp.challenge_id).toBe('c1');
  });

  it('verifyMFAChallenge POSTs { challenge_id, code } to verify-challenge', async () => {
    fetchMock.mockResolvedValue(makeJSON({ success: true, token: 'jwt' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.verifyMFAChallenge('t/1', 'u/1', 'c1', '654321');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/verify-challenge`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ challenge_id: 'c1', code: '654321' });
    expect(resp.token).toBe('jwt');
  });
});

describe('RestIAMClient MFA backup codes', () => {
  it('getBackupCodeStatus GETs backup-codes with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON({ total_codes: 10, remaining_codes: 7 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.getBackupCodeStatus('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/backup-codes`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.remaining_codes).toBe(7);
  });

  it('generateBackupCodes POSTs to backup-codes with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON({ codes: ['a', 'b'] }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.generateBackupCodes('t/1', 'u/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/backup-codes`);
    expect(init?.method).toBe('POST');
    expect(init?.body).toBeUndefined();
    expect(resp.codes).toEqual(['a', 'b']);
  });
});

describe('RestIAMClient MFA errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.getMFAStatus('t1', 'someone-else')).rejects.toBeInstanceOf(APIError);
  });
});
