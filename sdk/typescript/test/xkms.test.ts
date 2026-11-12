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

// Tests for RestXKMSClient: each method's URL, HTTP method and body.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const BASE = 'http://localhost:8080';
const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function make200JSON(payload: unknown) {
  return {
    ok: true,
    status: 200,
    statusText: 'OK',
    json: async () => payload,
    text: async () => JSON.stringify(payload),
  };
}

function make200Empty() {
  return {
    ok: true,
    status: 200,
    statusText: 'OK',
    json: async () => { throw new SyntaxError('Unexpected end of JSON input'); },
    text: async () => '',
  };
}

function make403() {
  return {
    ok: false,
    status: 403,
    statusText: 'Forbidden',
    json: async () => ({ error: 'forbidden' }),
    text: async () => '{"error":"forbidden"}',
  };
}

function call(i = 0): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[i] as [string, RequestInit | undefined];
  return {
    url,
    method: init?.method ?? 'GET',
    body: init?.body === undefined ? undefined : JSON.parse(init.body as string),
  };
}

const client = () => new RestClient(BASE, { token: 'tok' });

describe('RestXKMSClient', () => {
  it('listBackends GETs /api/v1/xkms/backends', async () => {
    const payload = { backends: [{ id: 'pkcs11', type: 'pkcs11', hardware_backed: true, capabilities: { Sealing: true } }], total: 1 };
    fetchMock.mockResolvedValue(make200JSON(payload));
    const resp = await client().xkms.listBackends();
    expect(call()).toEqual({ url: `${BASE}/api/v1/xkms/backends`, method: 'GET', body: undefined });
    expect(resp.backends[0].capabilities.Sealing).toBe(true);
    expect(resp.total).toBe(1);
  });

  it('getBackend encodes the id', async () => {
    fetchMock.mockResolvedValue(make200JSON({ id: 'a/b', type: 'sw', hardware_backed: false, capabilities: {} }));
    const resp = await client().xkms.getBackend('a/b');
    expect(call()).toEqual({ url: `${BASE}/api/v1/xkms/backends/a%2Fb`, method: 'GET', body: undefined });
    expect(resp.id).toBe('a/b');
  });

  it('health GETs /api/v1/xkms/health', async () => {
    fetchMock.mockResolvedValue(make200JSON({ status: 'ok', version: 'v1' }));
    const resp = await client().xkms.health();
    expect(call().url).toBe(`${BASE}/api/v1/xkms/health`);
    expect(resp.status).toBe('ok');
  });

  it('canSeal sends the backend query only when given', async () => {
    fetchMock.mockResolvedValue(make200JSON({ can_seal: true, backend: 'tpm 2' }));
    await client().xkms.canSeal();
    await client().xkms.canSeal('tpm 2');
    expect(call(0).url).toBe(`${BASE}/api/v1/xkms/seal/capability`);
    expect(call(1).url).toBe(`${BASE}/api/v1/xkms/seal/capability?backend=tpm%202`);
  });

  it('seal POSTs the request', async () => {
    fetchMock.mockResolvedValue(make200JSON({ backend: 'sw', ciphertext: 'Y3Q=', key_version: 2 }));
    const req = { backend: 'sw', key_id: 'k1', data: 'ZGF0YQ==', aad: 'YQ==' };
    const resp = await client().xkms.seal(req);
    expect(call()).toEqual({ url: `${BASE}/api/v1/xkms/seal`, method: 'POST', body: req });
    expect(resp.key_version).toBe(2);
  });

  it('unseal POSTs the request', async () => {
    fetchMock.mockResolvedValue(make200JSON({ plaintext: 'ZGF0YQ==' }));
    const req = { backend: 'sw', key_id: 'k1', ciphertext: 'Y3Q=', nonce: 'bg==', metadata: { a: 'b' } };
    const resp = await client().xkms.unseal(req);
    expect(call()).toEqual({ url: `${BASE}/api/v1/xkms/unseal`, method: 'POST', body: req });
    expect(resp.plaintext).toBe('ZGF0YQ==');
  });

  it('listCertificates sends the backend query only when given', async () => {
    fetchMock.mockResolvedValue(make200JSON({ certificates: [{ key_id: 'k1' }] }));
    const resp = await client().xkms.listCertificates();
    await client().xkms.listCertificates('a&b');
    expect(call(0).url).toBe(`${BASE}/api/v1/xkms/certs`);
    expect(call(1).url).toBe(`${BASE}/api/v1/xkms/certs?backend=a%26b`);
    expect(resp.certificates[0].key_id).toBe('k1');
  });

  it('getCertificate encodes the key id', async () => {
    fetchMock.mockResolvedValue(make200JSON({ key_id: 't/k', certificate_pem: 'PEM' }));
    const resp = await client().xkms.getCertificate('t/k');
    expect(call()).toEqual({ url: `${BASE}/api/v1/xkms/certs/t%2Fk`, method: 'GET', body: undefined });
    expect(resp.certificate_pem).toBe('PEM');
  });

  it('saveCertificate POSTs the PEM with the key_id query and resolves on an empty 200', async () => {
    fetchMock.mockResolvedValue(make200Empty());
    await expect(client().xkms.saveCertificate('t/k', 'PEM')).resolves.toBeUndefined();
    expect(call()).toEqual({
      url: `${BASE}/api/v1/xkms/certs?key_id=t%2Fk`,
      method: 'POST',
      body: { certificate_pem: 'PEM' },
    });
  });

  it('deleteCertificate DELETEs and resolves on an empty 200', async () => {
    fetchMock.mockResolvedValue(make200Empty());
    await expect(client().xkms.deleteCertificate('t/k')).resolves.toBeUndefined();
    expect(call()).toEqual({ url: `${BASE}/api/v1/xkms/certs/t%2Fk`, method: 'DELETE', body: undefined });
  });

  it('a 403 rejects with the status', async () => {
    fetchMock.mockResolvedValue(make403());
    await expect(client().xkms.health()).rejects.toMatchObject({ status: 403 });
  });

  it('sends the bearer token', async () => {
    fetchMock.mockResolvedValue(make200JSON({ status: 'ok' }));
    await client().xkms.health();
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer tok');
  });
});
