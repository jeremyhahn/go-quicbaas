// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Tests for the CeremonyClient REST surface (Phase 8 unification).
// All ceremony endpoints are mounted under /api/v1/init.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError, UnsupportedArgumentError } from '../src/errors';
import type { CeremonyOfficer } from '../src/types';

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

  // The REST ceremony handler reads only username and csr_pem; every other
  // officer option is refused before any request rather than silently dropped.
  const options: Array<[string, Partial<CeremonyOfficer>]> = [
    ['role', { role: 'custodian' }],
    ['encryption_kem', { encryption_kem: 'ML-KEM-768' }],
    ['encryption_key', { encryption_key: 'a2V5' }],
    ['factors', { factors: [{ name: 'fido2', params: { rp_id: 'node.example' } }] }],
    ['designated', { designated: 'fido2' }],
  ];

  it.each(options)('refuses an officer that sets %s', async (field, option) => {
    const officer: CeremonyOfficer = { username: 'custodian-1', csr_pem: 'CSR-1', ...option };

    const call = new RestClient('http://localhost').ceremony.initialize({
      so_pin: 'x',
      user_pin: 'y',
      threshold: 1,
      officers: [{ username: 'officer-1', csr_pem: 'CSR-0' }, officer],
    });

    await expect(call).rejects.toBeInstanceOf(UnsupportedArgumentError);
    await expect(call).rejects.toMatchObject({ transport: 'rest', argument: field });
    await expect(call).rejects.toThrow('REST ceremony backend does not support it yet');
    expect(fetchMock).not.toHaveBeenCalled();
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

describe('RestCeremonyClient certificate claim flow', () => {
  it('begins the certificate claim challenge', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        nonce_hex: 'deadbeef',
        expires_at: '2026-04-30T13:00:00Z',
      }),
    );

    const client = new RestClient('http://localhost');
    const challenge = await client.ceremony.claimCertBegin({ username: 'alice' });

    expect(challenge.nonce_hex).toBe('deadbeef');
    expect(challenge.expires_at).toBe('2026-04-30T13:00:00Z');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/claim-cert/begin');
  });

  it('completes the certificate claim with a signed nonce', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        cert_pem: '-----BEGIN CERTIFICATE-----',
        ca_cert_pem: '-----BEGIN CERTIFICATE-----',
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.claimCertComplete({
      username: 'alice',
      nonce_hex: 'deadbeef',
      signature: 'base64sig',
    });

    expect(result.cert_pem).toContain('CERTIFICATE');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/claim-cert/complete');
    expect(JSON.parse(init.body as string)).toEqual({
      username: 'alice',
      nonce_hex: 'deadbeef',
      signature: 'base64sig',
    });
  });
});

describe('RestCeremonyClient.claimShareBegin/claimShareComplete', () => {
  it('begins the share claim with the officer username only', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ nonce_hex: 'cafe', expires_at: '2026-10-18T13:00:00Z' }));

    const client = new RestClient('http://localhost');
    const challenge = await client.ceremony.claimShareBegin({ username: 'alice' });

    expect(challenge).toEqual({ nonce_hex: 'cafe', expires_at: '2026-10-18T13:00:00Z' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/officer-share/begin');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ username: 'alice' });
  });

  it('completes the share claim with the signed nonce', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ share_index: 2, share_json: '{"x":1}' }));

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.claimShareComplete({
      username: 'alice',
      nonce_hex: 'cafe',
      signature: 'c2ln',
    });

    expect(result).toEqual({ share_index: 2, share_json: '{"x":1}' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/officer-share/complete');
    expect(JSON.parse(init.body as string)).toEqual({ username: 'alice', nonce_hex: 'cafe', signature: 'c2ln' });
  });

  it('surfaces a server refusal of the completion as APIError', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: 'signature does not verify' }, 401));

    const client = new RestClient('http://localhost');
    const err = await client.ceremony
      .claimShareComplete({ username: 'alice', nonce_hex: 'cafe', signature: 'c2ln' })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(401);
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

describe('RestCeremonyClient.enrollExternalOfficer', () => {
  it('enrols an external officer with a public key and seals a share', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ sealed_share: 'base64sealed' }));

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.enrollExternalOfficer({
      username: 'extern-1',
      pubkey_pem: '-----BEGIN PUBLIC KEY-----',
      share_index: 4,
      auth_factor: 'webauthn',
      share_bytes: 'base64share',
    });

    expect(result.sealed_share).toBe('base64sealed');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/init/enroll-external-officer');
    expect(JSON.parse(init.body as string).username).toBe('extern-1');
  });

  it('enrols an external officer with a certificate and no share', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}));

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.enrollExternalOfficer({
      username: 'extern-2',
      cert_pem: '-----BEGIN CERTIFICATE-----',
      share_index: 5,
      auth_factor: 'mtls',
    });

    expect(result.sealed_share).toBeUndefined();
  });

  it('enrols an external officer with the PIV auth factor', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ sealed_share: 'piv-sealed' }));

    const client = new RestClient('http://localhost');
    const result = await client.ceremony.enrollExternalOfficer({
      username: 'piv-officer',
      cert_pem: '-----BEGIN CERTIFICATE-----',
      share_index: 7,
      auth_factor: 'piv',
      share_bytes: 'plaintext-share',
    });

    expect(result.sealed_share).toBe('piv-sealed');

    const [, init] = fetchMock.mock.calls[0];
    const body = JSON.parse(init.body as string);
    expect(body.auth_factor).toBe('piv');
    expect(body.share_index).toBe(7);
  });

  it('throws APIError on bad request', async () => {
    fetchMock.mockResolvedValue(errorResponse(400, 'invalid auth factor'));

    const client = new RestClient('http://localhost');
    await expect(
      client.ceremony.enrollExternalOfficer({
        username: 'x',
        pubkey_pem: 'PEM',
        share_index: 1,
        auth_factor: 'unknown',
      }),
    ).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// Full claim-cert flow — challenge then complete in sequence.
// Mirrors the daemon's nonce-bound flow: a fresh client must succeed when the
// nonce returned by Begin is presented to Complete.
// =============================================================================

describe('RestCeremonyClient certificate claim flow — end-to-end sequence', () => {
  it('begins a challenge then completes the claim with the same nonce', async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse({
          nonce_hex: 'cafef00d',
          expires_at: '2026-04-30T13:00:00Z',
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          cert_pem: '-----BEGIN CERTIFICATE-----',
          ca_cert_pem: '-----BEGIN CERTIFICATE-----',
        }),
      );

    const client = new RestClient('http://localhost');
    const begin = await client.ceremony.claimCertBegin({ username: 'alice' });
    expect(begin.nonce_hex).toBe('cafef00d');

    const complete = await client.ceremony.claimCertComplete({
      username: 'alice',
      nonce_hex: begin.nonce_hex,
      signature: 'base64sig',
    });
    expect(complete.cert_pem).toContain('CERTIFICATE');

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const [beginUrl] = fetchMock.mock.calls[0];
    const [completeUrl, completeInit] = fetchMock.mock.calls[1];
    expect(beginUrl).toBe('http://localhost/api/v1/init/claim-cert/begin');
    expect(completeUrl).toBe('http://localhost/api/v1/init/claim-cert/complete');
    const completeBody = JSON.parse(completeInit.body as string);
    expect(completeBody.nonce_hex).toBe(begin.nonce_hex);
  });

  it('reports an expired nonce on completion', async () => {
    fetchMock
      .mockResolvedValueOnce(
        jsonResponse({ nonce_hex: 'beef', expires_at: '2026-04-30T12:00:00Z' }),
      )
      .mockResolvedValueOnce(errorResponse(400, 'nonce expired'));

    const client = new RestClient('http://localhost');
    await client.ceremony.claimCertBegin({ username: 'alice' });
    await expect(
      client.ceremony.claimCertComplete({
        username: 'alice',
        nonce_hex: 'beef',
        signature: 'sig',
      }),
    ).rejects.toBeInstanceOf(APIError);
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
