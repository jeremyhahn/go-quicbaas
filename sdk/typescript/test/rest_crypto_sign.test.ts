// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestCryptoClient.sign: the RSA-PSS padding fields reach the
// POST body of /api/v1/crypto/sign/{name}, and are absent when not set.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';

function makeFetch(): ReturnType<typeof vi.fn> {
  return vi.fn(async () =>
    new Response(
      JSON.stringify({
        key_id: 'k1',
        key_version: 1,
        signature: 'c2ln',
        hash_algorithm: 'SHA256',
        timestamp: 0,
      }),
      {
        status: 200,
        headers: { 'content-type': 'application/json' },
      },
    ),
  );
}

function sentBody(fetchMock: ReturnType<typeof vi.fn>): Record<string, unknown> {
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(String(url)).toContain('/api/v1/crypto/sign/k1');
  expect(init.method).toBe('POST');
  return JSON.parse(String(init.body)) as Record<string, unknown>;
}

describe('RestCryptoClient.sign', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('sends the RSA-PSS padding fields', async () => {
    const fetchMock = makeFetch();
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    await client.crypto.sign({
      key_name: 'k1',
      message: 'bWVzc2FnZQ==',
      hash_algorithm: 'SHA256',
      padding: 'PSS',
      pss_salt_length: 32,
      mgf1_hash: 'SHA256',
    });

    const body = sentBody(fetchMock);
    expect(body.padding).toBe('PSS');
    expect(body.pss_salt_length).toBe(32);
    expect(body.mgf1_hash).toBe('SHA256');
  });

  it('omits padding when it is not set', async () => {
    const fetchMock = makeFetch();
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    await client.crypto.sign({ key_name: 'k1', message: 'bWVzc2FnZQ==' });

    const body = sentBody(fetchMock);
    expect('padding' in body).toBe(false);
    expect('pss_salt_length' in body).toBe(false);
    expect('mgf1_hash' in body).toBe(false);
  });
});
