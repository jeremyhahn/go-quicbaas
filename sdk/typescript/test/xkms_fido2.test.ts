// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// The REST xkms FIDO2 client: each method's URL, HTTP method and body, the
// optional encoded tenant_id query, and a 403 rejection.

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

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function lastCall(): { url: string; init: RequestInit } {
  const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
  return { url: String(call[0]), init: (call[1] ?? {}) as RequestInit };
}

const challenge = { session_id: 's-1', options_json: '{"publicKey":{}}' };

describe('RestXKMSFIDO2Client', () => {
  it('beginRegistration posts the body without tenant_id when none is given', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, challenge));
    const client = new RestClient('http://localhost');
    const req = { email: 'a@example.com', display_name: 'Ann' };
    const got = await client.xkmsFido2.beginRegistration(req);
    expect(got).toEqual(challenge);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/xkms/fido2/register/begin');
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toEqual(req);
  });

  it('beginRegistration sends an encoded tenant_id when given', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, challenge));
    const client = new RestClient('http://localhost');
    await client.xkmsFido2.beginRegistration({ email: 'a@example.com', display_name: 'Ann' }, 'acme corp/1');
    expect(lastCall().url).toBe(
      'http://localhost/api/v1/xkms/fido2/register/begin?tenant_id=acme%20corp%2F1',
    );
  });

  it('finishRegistration posts the attestation', async () => {
    const resp = { success: true, credential_id: 'c-1', token: 't' };
    fetchMock.mockResolvedValue(jsonResponse(200, resp));
    const client = new RestClient('http://localhost');
    const req = { session_id: 's-1', attestation_json: '{"id":"x"}' };
    const got = await client.xkmsFido2.finishRegistration(req, 't1');
    expect(got).toEqual(resp);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/xkms/fido2/register/finish?tenant_id=t1');
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toEqual(req);
  });

  it('finishRegistration omits tenant_id when none is given', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { success: true, credential_id: 'c-1' }));
    const client = new RestClient('http://localhost');
    await client.xkmsFido2.finishRegistration({ session_id: 's-1', attestation_json: '{}' });
    expect(lastCall().url).toBe('http://localhost/api/v1/xkms/fido2/register/finish');
  });

  it('beginAddCredential posts with no body and no query', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, challenge));
    const client = new RestClient('http://localhost');
    const got = await client.xkmsFido2.beginAddCredential();
    expect(got).toEqual(challenge);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/xkms/fido2/credentials/begin');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
  });

  it('beginAuthentication posts the email', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, challenge));
    const client = new RestClient('http://localhost');
    await client.xkmsFido2.beginAuthentication({ email: 'a@example.com' });
    let call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/xkms/fido2/authenticate/begin');
    expect(call.init.method).toBe('POST');
    expect(JSON.parse(String(call.init.body))).toEqual({ email: 'a@example.com' });

    fetchMock.mockResolvedValue(jsonResponse(200, challenge));
    await client.xkmsFido2.beginAuthentication({ email: 'a@example.com' }, 'a&b');
    call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/xkms/fido2/authenticate/begin?tenant_id=a%26b');
  });

  it('finishAuthentication posts the assertion', async () => {
    const resp = { success: true, token: 'jwt', username: 'ann' };
    fetchMock.mockResolvedValue(jsonResponse(200, resp));
    const client = new RestClient('http://localhost');
    const req = { session_id: 's-1', email: 'a@example.com', assertion_json: '{"id":"y"}' };
    const got = await client.xkmsFido2.finishAuthentication(req);
    expect(got).toEqual(resp);
    let call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/xkms/fido2/authenticate/finish');
    expect(call.init.method).toBe('POST');
    expect(JSON.parse(String(call.init.body))).toEqual(req);

    fetchMock.mockResolvedValue(jsonResponse(200, resp));
    await client.xkmsFido2.finishAuthentication(req, 't 2');
    call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/xkms/fido2/authenticate/finish?tenant_id=t%202');
  });

  it('rejects on 403', async () => {
    fetchMock.mockResolvedValue(jsonResponse(403, { error: 'forbidden' }));
    const client = new RestClient('http://localhost');
    await expect(client.xkmsFido2.beginAddCredential()).rejects.toThrow();
  });
});
