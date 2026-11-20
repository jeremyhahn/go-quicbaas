// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestAttestationClient: each method's URL (path segments
// encoded), HTTP method and body, the kind query, and a 403 rejection.

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

type FakeResponse = {
  ok: boolean;
  status: number;
  statusText: string;
  headers: Headers;
  json: () => Promise<unknown>;
  text: () => Promise<string>;
};

function respond(status: number, payload?: unknown): FakeResponse {
  const body = payload === undefined ? '' : JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 403 ? 'Forbidden' : 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => JSON.parse(body),
    text: async () => body,
  };
}

function lastCall(): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit | undefined];
  return {
    url: String(url),
    method: init?.method ?? 'GET',
    body: init?.body === undefined ? undefined : JSON.parse(String(init.body)),
  };
}

const BASE = 'http://localhost';
const NAME = 'tpm/root ca';
const ENC = encodeURIComponent(NAME);

const endorsement = {
  name: NAME,
  kind: 'ak-root',
  certificate_pem: '-----BEGIN CERTIFICATE-----',
  created_at: '2026-11-12T00:00:00Z',
};

const refValues = {
  name: NAME,
  manifest_id: 'm-1',
  pcr_indices: [0, 7],
  stored_at: '2026-11-12T00:00:00Z',
  signed_corim: 'c2lnbmVk',
};

describe('RestAttestationClient', () => {
  it('putEndorsement PUTs the body to the encoded name', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, endorsement));
    const client = new RestClient(BASE);
    const req = { kind: 'ak-root' as const, certificate_pem: '-----BEGIN CERTIFICATE-----' };
    const got = await client.attestation.putEndorsement(NAME, req);
    expect(got).toEqual(endorsement);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/endorsements/${ENC}`);
    expect(c.url).toContain('tpm%2Froot%20ca');
    expect(c.method).toBe('PUT');
    expect(c.body).toEqual(req);
  });

  it('listEndorsements GETs without a query', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { endorsements: [endorsement] }));
    const client = new RestClient(BASE);
    const got = await client.attestation.listEndorsements();
    expect(got.endorsements).toEqual([endorsement]);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/endorsements`);
    expect(c.method).toBe('GET');
    expect(c.body).toBeUndefined();
  });

  it('listEndorsements sends the kind query', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { endorsements: null }));
    const client = new RestClient(BASE);
    const got = await client.attestation.listEndorsements('reference-signer');
    expect(got.endorsements).toEqual([]);
    expect(lastCall().url).toBe(`${BASE}/api/v1/attestation/endorsements?kind=reference-signer`);
  });

  it('deleteEndorsement DELETEs the encoded name and resolves on 204', async () => {
    fetchMock.mockResolvedValueOnce(respond(204));
    const client = new RestClient(BASE);
    await expect(client.attestation.deleteEndorsement(NAME)).resolves.toBeUndefined();
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/endorsements/${ENC}`);
    expect(c.method).toBe('DELETE');
  });

  it('putReferenceValues PUTs signed_corim to the encoded name', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, refValues));
    const client = new RestClient(BASE);
    const got = await client.attestation.putReferenceValues(NAME, 'c2lnbmVk');
    expect(got).toEqual(refValues);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/reference-values/${ENC}`);
    expect(c.method).toBe('PUT');
    expect(c.body).toEqual({ signed_corim: 'c2lnbmVk' });
  });

  it('getReferenceValues GETs the encoded name', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, refValues));
    const client = new RestClient(BASE);
    const got = await client.attestation.getReferenceValues(NAME);
    expect(got.signed_corim).toBe('c2lnbmVk');
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/reference-values/${ENC}`);
    expect(c.method).toBe('GET');
  });

  it('listReferenceValues GETs the collection', async () => {
    const { signed_corim: _omit, ...listed } = refValues;
    fetchMock.mockResolvedValueOnce(respond(200, { reference_values: [listed] }));
    const client = new RestClient(BASE);
    const got = await client.attestation.listReferenceValues();
    expect(got.reference_values).toEqual([listed]);
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/reference-values`);
    expect(c.method).toBe('GET');
  });

  it('deleteReferenceValues DELETEs the encoded name and resolves on 204', async () => {
    fetchMock.mockResolvedValueOnce(respond(204));
    const client = new RestClient(BASE);
    await expect(client.attestation.deleteReferenceValues(NAME)).resolves.toBeUndefined();
    const c = lastCall();
    expect(c.url).toBe(`${BASE}/api/v1/attestation/reference-values/${ENC}`);
    expect(c.method).toBe('DELETE');
  });

  it('rejects on 403', async () => {
    fetchMock.mockResolvedValueOnce(respond(403, { error: 'forbidden' }));
    const client = new RestClient(BASE);
    await expect(client.attestation.listEndorsements()).rejects.toThrow();
  });
});
