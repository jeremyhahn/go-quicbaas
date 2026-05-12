// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression test for RestSecretsClient.listSecrets: the server's
// /api/v1/secrets endpoint returns entries shaped
// {secret_id, name, current_version, created_at, updated_at}, but the SDK
// surface (and every UI consumer) expects {path, version, created_at,
// updated_at}. Without adaptation, secret rows render with blank Path /
// Version columns and "Fetch secret undefined" aria-labels. This test
// pins the wire-shape adapter so that bug cannot recur.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';

function makeFetch(body: unknown): typeof globalThis.fetch {
  return vi.fn(async () =>
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }),
  ) as unknown as typeof globalThis.fetch;
}

describe('RestSecretsClient.listSecrets wire-shape adapter', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('translates secret_id → path and current_version → version', async () => {
    globalThis.fetch = makeFetch({
      secrets: [
        {
          secret_id: 'app/database/password',
          name: 'app/database/password',
          current_version: 3,
          created_at: '2026-05-03T10:00:00Z',
          updated_at: '2026-05-03T11:00:00Z',
        },
      ],
      total_count: 1,
      has_more: false,
    });
    const client = new RestClient('http://localhost:18080');
    const resp = await client.secrets.listSecrets();
    expect(resp.secrets).toHaveLength(1);
    expect(resp.secrets[0].path).toBe('app/database/password');
    expect(resp.secrets[0].version).toBe(3);
    expect(resp.secrets[0].created_at).toBe('2026-05-03T10:00:00Z');
    expect(resp.secrets[0].updated_at).toBe('2026-05-03T11:00:00Z');
    expect(resp.count).toBe(1);
  });

  it('falls back to name when secret_id is missing', async () => {
    globalThis.fetch = makeFetch({
      secrets: [{ name: 'fallback/path', current_version: 1 }],
      total_count: 1,
    });
    const client = new RestClient('http://localhost:18080');
    const resp = await client.secrets.listSecrets();
    expect(resp.secrets[0].path).toBe('fallback/path');
  });

  it('passes through entries that already have path/version', async () => {
    globalThis.fetch = makeFetch({
      secrets: [{ path: 'already/correct', version: 7, created_at: 'a', updated_at: 'b' }],
      count: 1,
    });
    const client = new RestClient('http://localhost:18080');
    const resp = await client.secrets.listSecrets();
    expect(resp.secrets[0].path).toBe('already/correct');
    expect(resp.secrets[0].version).toBe(7);
  });

  it('derives count from secrets[].length when total_count and count are absent', async () => {
    globalThis.fetch = makeFetch({
      secrets: [
        { secret_id: 'a', current_version: 1 },
        { secret_id: 'b', current_version: 1 },
        { secret_id: 'c', current_version: 1 },
      ],
    });
    const client = new RestClient('http://localhost:18080');
    const resp = await client.secrets.listSecrets();
    expect(resp.count).toBe(3);
  });

  it('regression guard: row never has blank path even when wire shape drifts', async () => {
    globalThis.fetch = makeFetch({
      secrets: [{ secret_id: 'p', current_version: 0, created_at: '', updated_at: '' }],
    });
    const client = new RestClient('http://localhost:18080');
    const resp = await client.secrets.listSecrets();
    expect(resp.secrets[0].path).not.toBe('');
    expect(resp.secrets[0].path).toBe('p');
  });
});
