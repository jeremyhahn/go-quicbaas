// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Tests for the BootstrapClient REST surface (Phase 8 unification).
// Each method maps to POST /api/v1/bootstrap/{auto,dane,noise,spki}.

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

describe('RestBootstrapClient.auto', () => {
  it('retrieves a CA bundle through the auto method', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        method: 'auto',
        bundle_pem: '-----BEGIN CERTIFICATE-----',
        content_type: 'application/pem-certificate-chain',
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.bootstrap.auto();

    expect(result.method).toBe('auto');
    expect(result.bundle_pem).toContain('CERTIFICATE');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/bootstrap/auto');
    expect(init.method).toBe('POST');
  });

  it('passes filters through to the daemon', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        method: 'auto',
        bundle_pem: 'PEM',
        content_type: 'application/pem-certificate-chain',
      }),
    );

    const client = new RestClient('http://localhost');
    await client.bootstrap.auto({ store_type: 'root', algorithm: 'Ed25519' });

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({
      store_type: 'root',
      algorithm: 'Ed25519',
    });
  });

  it('throws APIError when no bootstrap method is configured', async () => {
    fetchMock.mockResolvedValue(errorResponse(503, 'no bootstrap method available'));

    const client = new RestClient('http://localhost');
    await expect(client.bootstrap.auto()).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestBootstrapClient.dane', () => {
  it('retrieves a CA bundle through DANE', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        method: 'dane',
        bundle_pem: 'PEM-DANE',
        content_type: 'application/pem-certificate-chain',
        metadata: { dane_validated: 'true' },
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.bootstrap.dane();

    expect(result.method).toBe('dane');
    expect(result.metadata?.dane_validated).toBe('true');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/bootstrap/dane');
  });

  it('returns 503 when DANE is not configured', async () => {
    fetchMock.mockResolvedValue(errorResponse(503, 'dane not configured'));

    const client = new RestClient('http://localhost');
    await expect(client.bootstrap.dane()).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestBootstrapClient.noise', () => {
  it('retrieves a CA bundle through the Noise protocol', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        method: 'noise',
        bundle_pem: 'PEM-NOISE',
        content_type: 'application/pem-certificate-chain',
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.bootstrap.noise();

    expect(result.method).toBe('noise');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/bootstrap/noise');
  });
});

describe('RestBootstrapClient.spki', () => {
  it('retrieves a CA bundle through SPKI pinning', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        method: 'spki',
        bundle_pem: 'PEM-SPKI',
        content_type: 'application/pem-certificate-chain',
        certificates: ['DER1', 'DER2'],
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.bootstrap.spki();

    expect(result.method).toBe('spki');
    expect(result.certificates).toHaveLength(2);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/bootstrap/spki');
  });
});

describe('RestBootstrapClient — strategy coverage', () => {
  it.each(['auto', 'dane', 'noise', 'spki'] as const)(
    'covers the %s strategy with a happy-path response',
    async (method) => {
      fetchMock.mockResolvedValue(
        jsonResponse({
          method,
          bundle_pem: `PEM-${method}`,
          content_type: 'application/pem-certificate-chain',
        }),
      );

      const client = new RestClient('http://localhost');
      const result = await client.bootstrap[method]();

      expect(result.method).toBe(method);
      const [url] = fetchMock.mock.calls[0];
      expect(url).toBe(`http://localhost/api/v1/bootstrap/${method}`);
    },
  );
});
