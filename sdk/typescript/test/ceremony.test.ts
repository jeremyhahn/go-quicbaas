// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Tests for the CeremonyClient REST surface (Phase 8 unification).
// All ceremony endpoints are mounted under /api/v1/init.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function errorResponse(status: number, message = 'error'): Response {
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

describe('RestCeremonyClient.initialize', () => {
  it('initialises the ceremony in single-admin mode', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        spki_pin: 'sha256-abc',
        ca_cert_pem: '-----BEGIN CERTIFICATE-----',
        tls_cert_pem: '-----BEGIN CERTIFICATE-----',
        state: 'complete',
        share_count: 0,
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.initialize({
      so_pin: 'so-pin',
      user_pin: 'user-pin',
      threshold: 0,
    });

    expect(result.spki_pin).toBe('sha256-abc');
    expect(result.share_count).toBe(0);
    expect(result.state).toBe('complete');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/initialize');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toMatchObject({
      so_pin: 'so-pin',
      user_pin: 'user-pin',
      threshold: 0,
    });
  });

  it('initialises the ceremony with M-of-N officers', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        spki_pin: 'sha256-abc',
        ca_cert_pem: '-----BEGIN CERTIFICATE-----',
        tls_cert_pem: '-----BEGIN CERTIFICATE-----',
        state: 'awaiting-claims',
        share_count: 5,
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.initialize({
      so_pin: 'so',
      user_pin: 'user',
      threshold: 3,
      officers: [
        { username: 'alice', csr_pem: 'CSR-A' },
        { username: 'bob', csr_pem: 'CSR-B' },
      ],
      hostname: 'example.com',
      sans: ['example.com', 'www.example.com'],
    });

    expect(result.share_count).toBe(5);
    expect(result.state).toBe('awaiting-claims');
  });

  it('throws APIError when the ceremony is sealed', async () => {
    fetchMock.mockResolvedValue(errorResponse(503, 'barrier sealed'));

    const client = new RestClient('http://localhost');
    await expect(
      client.ceremony.initialize({ so_pin: 'x', user_pin: 'y', threshold: 0 }),
    ).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestCeremonyClient.status', () => {
  it('returns the current ceremony state', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ state: 'complete', spki_pin: 'sha256-abc' }));

    const client = new RestClient('http://localhost');
    const info = await client.ceremony.status();

    expect(info.state).toBe('complete');
    expect(info.spki_pin).toBe('sha256-abc');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/status');
  });

  it('reports uninitialised state when CA has not been generated', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ state: 'uninitialized' }));

    const client = new RestClient('http://localhost');
    const info = await client.ceremony.status();

    expect(info.state).toBe('uninitialized');
    expect(info.spki_pin).toBeUndefined();
  });
});
describe('RestCeremonyClient.signCSR', () => {
  it('signs a CSR during the ceremony', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ cert_pem: '-----BEGIN CERTIFICATE-----' }));

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.signCSR({
      username: 'service-a',
      so_pin: 'so',
      csr_pem: 'CSR',
      role: 'service',
    });

    expect(result.cert_pem).toContain('CERTIFICATE');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/sign-csr');
    expect(JSON.parse(init.body as string)).toEqual({
      username: 'service-a',
      so_pin: 'so',
      csr_pem: 'CSR',
      role: 'service',
    });
  });
});
// =============================================================================
// Restart persistence — a freshly constructed client must observe the daemon's
// persisted state. Since the SDK is stateless, "restart persistence" maps onto
// constructing two clients against the same endpoint and confirming both
// observe identical server responses without holding any client-side cache.
// =============================================================================

describe('RestCeremonyClient restart persistence', () => {
  it('a new client instance observes the persisted ceremony state', async () => {
    // A Response body can only be read once, so each fetch call needs a
    // fresh Response. mockImplementation rebuilds the response per call.
    fetchMock.mockImplementation(() =>
      Promise.resolve(jsonResponse({ state: 'complete', spki_pin: 'sha256-persisted' })),
    );

    const before = new RestClient('http://localhost');
    const beforeStatus = await before.ceremony.status();
    expect(beforeStatus.state).toBe('complete');
    expect(beforeStatus.spki_pin).toBe('sha256-persisted');

    // Simulate a daemon restart by constructing a brand-new client instance.
    // Because the SDK holds no ceremony state, the second client must
    // re-fetch from the server and observe the same persisted result.
    const after = new RestClient('http://localhost');
    const afterStatus = await after.ceremony.status();
    expect(afterStatus.state).toBe('complete');
    expect(afterStatus.spki_pin).toBe('sha256-persisted');

    // Each call hit the daemon — the SDK never short-circuits.
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0][0]).toBe(
      'http://localhost/api/v1/init/status',
    );
    expect(fetchMock.mock.calls[1][0]).toBe(
      'http://localhost/api/v1/init/status',
    );
  });

  it('reports awaiting-claims after init when officers have not yet claimed', async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse({
          spki_pin: 'sha256-pin',
          ca_cert_pem: 'CA',
          tls_cert_pem: 'TLS',
          state: 'awaiting-claims',
          share_count: 3,
        }),
      )
      .mockResolvedValueOnce(jsonResponse({ state: 'awaiting-claims', spki_pin: 'sha256-pin' }));

    const client = new RestClient('http://localhost');
    const init = await client.ceremony.initialize({
      so_pin: 'so',
      user_pin: 'user',
      threshold: 2,
      officers: [
        { username: 'alice', csr_pem: 'A' },
        { username: 'bob', csr_pem: 'B' },
        { username: 'carol', csr_pem: 'C' },
      ],
    });
    expect(init.state).toBe('awaiting-claims');
    expect(init.share_count).toBe(3);

    // After a "restart" (new client instance) the state remains
    // awaiting-claims because no officer has yet claimed their cert.
    const fresh = new RestClient('http://localhost');
    const status = await fresh.ceremony.status();
    expect(status.state).toBe('awaiting-claims');
  });
});
