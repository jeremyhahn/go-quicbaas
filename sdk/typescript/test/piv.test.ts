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

// RestPIVClient: each method hits its /api/v1/xkms/piv/slots* route with the
// slot encoded as a path segment and the backend/format queries encoded.

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

function emptyResponse(status = 200): Response {
  return new Response('', { status });
}

function call(i = 0): { url: string; method: string; body: unknown } {
  const [input, init] = fetchMock.mock.calls[i] as [unknown, RequestInit | undefined];
  const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : (input as Request).url;
  const raw = init?.body;
  return {
    url,
    method: (init?.method ?? 'GET').toUpperCase(),
    body: typeof raw === 'string' && raw !== '' ? JSON.parse(raw) : undefined,
  };
}

function client() {
  return new RestClient('http://localhost').piv;
}

describe('RestPIVClient', () => {
  it('listSlots GETs the slots, with no query by default', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { slots: [{ slot: '9a', name: 'Authentication', description: 'PIV auth', has_cert: true, has_key: true }] }),
    );
    const res = await client().listSlots();
    const c = call();
    expect(c.method).toBe('GET');
    expect(c.url).toBe('http://localhost/api/v1/xkms/piv/slots');
    expect(res.slots).toHaveLength(1);
    expect(res.slots[0].slot).toBe('9a');
  });

  it('listSlots encodes the backend query and defaults slots to []', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {}));
    const res = await client().listSlots('yubi key&1');
    expect(call().url).toBe('http://localhost/api/v1/xkms/piv/slots?backend=yubi%20key%261');
    expect(res.slots).toEqual([]);
  });

  it('getCertificate encodes the slot and the backend and format queries', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { slot: '9a/x', certificate: 'Zm9v', format: 'der' }));
    const res = await client().getCertificate('9a/x', { backend: 'pkcs11', format: 'der' });
    const c = call();
    expect(c.method).toBe('GET');
    expect(c.url).toBe('http://localhost/api/v1/xkms/piv/slots/9a%2Fx/cert?backend=pkcs11&format=der');
    expect(res.certificate).toBe('Zm9v');
  });

  it('getCertificate sends no query without options', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { slot: '9c', certificate: 'PEM', format: 'pem' }));
    await client().getCertificate('9c');
    expect(call().url).toBe('http://localhost/api/v1/xkms/piv/slots/9c/cert');
  });

  it('storeCertificate PUTs the body and resolves on a 200 with an empty body', async () => {
    fetchMock.mockResolvedValue(emptyResponse(200));
    const req = { backend: 'pkcs11', certificate: '-----BEGIN CERTIFICATE-----', format: 'pem' as const };
    await expect(client().storeCertificate('9 d', req)).resolves.toBeUndefined();
    const c = call();
    expect(c.method).toBe('PUT');
    expect(c.url).toBe('http://localhost/api/v1/xkms/piv/slots/9%20d/cert');
    expect(c.body).toEqual(req);
  });

  it('deleteCertificate DELETEs with the backend query and resolves on a 200 with an empty body', async () => {
    fetchMock.mockResolvedValue(emptyResponse(200));
    await expect(client().deleteCertificate('9e', 'soft hsm')).resolves.toBeUndefined();
    const c = call();
    expect(c.method).toBe('DELETE');
    expect(c.url).toBe('http://localhost/api/v1/xkms/piv/slots/9e/cert?backend=soft%20hsm');
  });

  it('generateKey POSTs the body and returns the 201 answer', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(201, { slot: '9a', certificate: 'CERT', public_key: 'PUB', csr: 'CSR' }),
    );
    const req = {
      algorithm: 'ecdsap256' as const,
      subject: 'CN=alice',
      organization: 'ACME',
      validity_days: 365,
    };
    const res = await client().generateKey('9a?', req);
    const c = call();
    expect(c.method).toBe('POST');
    expect(c.url).toBe('http://localhost/api/v1/xkms/piv/slots/9a%3F/key');
    expect(c.body).toEqual(req);
    expect(res.public_key).toBe('PUB');
    expect(res.csr).toBe('CSR');
  });

  it('generateCSR POSTs the body and returns the 201 answer', async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, { slot: '9c', csr: 'CSRPEM' }));
    const req = { backend: 'pkcs11', subject: 'CN=bob', organizational_unit: 'Ops', locality: 'Austin' };
    const res = await client().generateCSR('9c', req);
    const c = call();
    expect(c.method).toBe('POST');
    expect(c.url).toBe('http://localhost/api/v1/xkms/piv/slots/9c/csr');
    expect(c.body).toEqual(req);
    expect(res.csr).toBe('CSRPEM');
  });

  it('rejects on a 403', async () => {
    fetchMock.mockResolvedValue(jsonResponse(403, { error: 'forbidden' }));
    await expect(client().deleteCertificate('9a')).rejects.toThrow();
  });
});
