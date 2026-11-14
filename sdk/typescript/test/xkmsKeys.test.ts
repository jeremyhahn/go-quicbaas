// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestXKMSKeysClient (client.xkmsKeys): each method's URL, HTTP
// method and body against /api/v1/xkms/keys/*.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

function makeJSON(status: number, payload: unknown) {
  const body = JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 403 ? 'Forbidden' : 'OK',
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

beforeEach(() => {
  fetchMock = vi.fn().mockResolvedValue(makeJSON(200, {}));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

const BASE = 'http://localhost';
const ID = 'tenant/app key';
const ENC = encodeURIComponent(ID);
const KEYS = `${BASE}/api/v1/xkms/keys`;

function lastCall(): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit | undefined];
  return {
    url,
    method: init?.method ?? 'GET',
    body: init?.body === undefined ? undefined : JSON.parse(init.body as string),
  };
}

describe('xkmsKeys client', () => {
  it('encodes an id containing a slash as one segment', () => {
    expect(ENC).toBe('tenant%2Fapp%20key');
  });

  it('listKeys: GET, with and without backend', async () => {
    const c = new RestClient(BASE);
    await c.xkmsKeys.listKeys();
    expect(lastCall()).toEqual({ url: KEYS, method: 'GET', body: undefined });
    await c.xkmsKeys.listKeys('pkcs11 a&b');
    expect(lastCall().url).toBe(`${KEYS}?backend=pkcs11%20a%26b`);
  });

  it('generateKey: POST body, resolves on a 201', async () => {
    fetchMock.mockResolvedValue(makeJSON(201, { key_id: 'k1', key_type: 'rsa' }));
    const c = new RestClient(BASE);
    const req = { key_id: 'k1', backend: 'pkcs8', key_type: 'rsa', key_size: 2048 };
    const res = await c.xkmsKeys.generateKey(req);
    expect(res).toEqual({ key_id: 'k1', key_type: 'rsa' });
    expect(lastCall()).toEqual({ url: KEYS, method: 'POST', body: req });
  });

  it('getKey: GET with backend query', async () => {
    const c = new RestClient(BASE);
    await c.xkmsKeys.getKey(ID);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}`, method: 'GET', body: undefined });
    await c.xkmsKeys.getKey(ID, 'tpm2');
    expect(lastCall().url).toBe(`${KEYS}/${ENC}?backend=tpm2`);
  });

  it('deleteKey: DELETE', async () => {
    await new RestClient(BASE).xkmsKeys.deleteKey(ID);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}`, method: 'DELETE', body: undefined });
  });

  it('rotateKey: POST no body', async () => {
    await new RestClient(BASE).xkmsKeys.rotateKey(ID);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/rotate`, method: 'POST', body: undefined });
  });

  it('encrypt / decrypt / sign / verify: POST bodies', async () => {
    const c = new RestClient(BASE).xkmsKeys;
    const enc = { plaintext: 'aGk=', additional_data: 'YQ==' };
    await c.encrypt(ID, enc);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/encrypt`, method: 'POST', body: enc });
    const dec = { ciphertext: 'Y3Q=', nonce: 'bg==', tag: 'dA==' };
    await c.decrypt(ID, dec);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/decrypt`, method: 'POST', body: dec });
    const sig = { data: 'ZA==', hash: 'SHA-256' };
    await c.sign(ID, sig);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/sign`, method: 'POST', body: sig });
    const ver = { data: 'ZA==', signature: 'cw==' };
    await c.verify(ID, ver);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/verify`, method: 'POST', body: ver });
  });

  it('key versions: list, enable-all, disable-all, enable, disable', async () => {
    const c = new RestClient(BASE).xkmsKeys;
    await c.listKeyVersions(ID);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/versions`, method: 'GET', body: undefined });
    await c.enableAllKeyVersions(ID);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/versions/enable-all`, method: 'POST', body: undefined });
    await c.disableAllKeyVersions(ID);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/versions/disable-all`, method: 'POST', body: undefined });
    await c.enableKeyVersion(ID, 3);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/versions/3/enable`, method: 'POST', body: undefined });
    await c.disableKeyVersion(ID, 4);
    expect(lastCall()).toEqual({ url: `${KEYS}/${ENC}/versions/4/disable`, method: 'POST', body: undefined });
  });

  it('rejects with APIError on a 403', async () => {
    fetchMock.mockResolvedValue(makeJSON(403, { error: 'permission denied' }));
    const p = new RestClient(BASE).xkmsKeys.deleteKey(ID);
    await expect(p).rejects.toBeInstanceOf(APIError);
    await expect(new RestClient(BASE).xkmsKeys.deleteKey(ID)).rejects.toMatchObject({ status: 403 });
  });
});
