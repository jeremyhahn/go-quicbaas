// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Tests for the BarrierClient REST surface (Phase 8 unification).
//
// The QuicBaaS daemon mounts the barrier endpoints under /api/v1/barrier;
// these tests stub global fetch and verify routing, request bodies, and
// response shapes for each method.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function noContentResponse(): Response {
  return new Response(null, { status: 204 });
}

function errorResponse(status: number, message = 'error'): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// =============================================================================
// status — simple GET, returns BarrierStatusResponse
// =============================================================================

describe('RestBarrierClient.status', () => {
  it('returns the barrier status when the daemon is sealed', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        sealed: true,
        strategy: 'shamir',
        hardware_backed: false,
      }),
    );

    const client = new RestClient('http://localhost');
    const status = await client.barrier.status();

    expect(status.sealed).toBe(true);
    expect(status.strategy).toBe('shamir');
    expect(status.hardware_backed).toBe(false);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/status');
    // GET requests don't carry an explicit method.
    expect(init.method).toBeUndefined();
  });

  it('returns the barrier status with initialized_at when unsealed', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        sealed: false,
        strategy: 'passphrase',
        hardware_backed: true,
        initialized_at: '2026-04-30T12:00:00Z',
      }),
    );

    const client = new RestClient('http://localhost');
    const status = await client.barrier.status();

    expect(status.sealed).toBe(false);
    expect(status.hardware_backed).toBe(true);
    expect(status.initialized_at).toBe('2026-04-30T12:00:00Z');
  });

  it('throws APIError on 503 sealed-window response', async () => {
    fetchMock.mockResolvedValue(errorResponse(503, 'barrier sealed'));

    const client = new RestClient('http://localhost');
    await expect(client.barrier.status()).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// initialize / unseal / seal — passphrase strategy
// =============================================================================

describe('RestBarrierClient passphrase strategy', () => {
  it('initialises the barrier with a passphrase', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.initialize({ secret: 'dG9wc2VjcmV0' });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/initialize');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ secret: 'dG9wc2VjcmV0' });
  });

  it('unseals the barrier with a passphrase', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.unseal({ secret: 'dG9wc2VjcmV0' });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/unseal');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ secret: 'dG9wc2VjcmV0' });
  });

  it('seals the barrier', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.seal();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/seal');
    expect(init.method).toBe('POST');
  });
});

// =============================================================================
// Shamir strategy — initialize, unseal, list, delete, verify
// =============================================================================

describe('RestBarrierClient Shamir strategy', () => {
  it('initialises the barrier with Shamir secret sharing', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        shares: ['share-1', 'share-2', 'share-3'],
        threshold: 2,
        total_shares: 3,
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.barrier.initializeShamir({
      secret: 'bWFzdGVy',
      threshold: 2,
      total_shares: 3,
    });

    expect(result.shares).toHaveLength(3);
    expect(result.threshold).toBe(2);
    expect(result.total_shares).toBe(3);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/initialize');
    expect(JSON.parse(init.body as string)).toEqual({
      secret: 'bWFzdGVy',
      threshold: 2,
      total_shares: 3,
    });
  });

  it('submits one share toward the unseal quorum', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        required: 3,
        submitted: 1,
        complete: false,
      }),
    );

    const client = new RestClient('http://localhost');
    const progress = await client.barrier.unsealWithShare({ share: 'share-A' });

    expect(progress.complete).toBe(false);
    expect(progress.submitted).toBe(1);
    expect(progress.required).toBe(3);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/unseal-share');
  });

  it('submits multiple shares in a single request', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.unsealWithShares({ shares: ['a', 'b', 'c'] });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/unseal-shares');
    expect(JSON.parse(init.body as string)).toEqual({ shares: ['a', 'b', 'c'] });
  });

  it('lists persisted Shamir share metadata', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        count: 3,
        threshold: 2,
        total: 3,
      }),
    );

    const client = new RestClient('http://localhost');
    const info = await client.barrier.shamirListShares();

    expect(info.count).toBe(3);
    expect(info.threshold).toBe(2);
    expect(info.total).toBe(3);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/shares');
  });

  it('deletes a single share by index', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.shamirDeleteShare({ index: 2 });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/shares/2');
    expect(init.method).toBe('DELETE');
  });

  it('deletes all shares', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.shamirDeleteAllShares();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/shares');
    expect(init.method).toBe('DELETE');
  });

  it('verifies share consistency', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.shamirVerify();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/shamir/verify');
    expect(init.method).toBe('POST');
  });
});

// =============================================================================
// Rekey, recovery, root token
// =============================================================================

describe('RestBarrierClient operator flows', () => {
  it('rekeys the barrier', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        shares: ['new-1', 'new-2'],
        threshold: 1,
        total_shares: 2,
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.barrier.rekey({ threshold: 1, total: 2 });

    expect(result.threshold).toBe(1);
    expect(result.total_shares).toBe(2);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/rekey');
  });

  it('generates recovery keys', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        keys: ['rec-1', 'rec-2', 'rec-3'],
        threshold: 2,
        total: 3,
      }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.barrier.generateRecoveryKeys({ threshold: 2, total: 3 });

    expect(result.keys).toHaveLength(3);
    expect(result.total).toBe(3);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/recovery/generate');
  });

  it('recovers the barrier with recovery keys', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.recoverWithKeys({ keys: ['k1', 'k2'] });

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/recovery/recover');
  });

  it('deletes recovery keys', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.deleteRecoveryKeys();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/recovery');
    expect(init.method).toBe('DELETE');
  });

  it('reports recovery key status', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ has_keys: true }));

    const client = new RestClient('http://localhost');
    const status = await client.barrier.recoveryStatus();

    expect(status.has_keys).toBe(true);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/recovery/status');
  });

  it('generates a root token from Shamir shares', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        token: 'root-deadbeef',
        created_at: '2026-04-30T12:00:00Z',
      }),
    );

    const client = new RestClient('http://localhost');
    const token = await client.barrier.generateRootToken({ shares: ['s1', 's2'] });

    expect(token.token).toBe('root-deadbeef');
    expect(token.created_at).toBe('2026-04-30T12:00:00Z');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/root-token');
  });
});

// =============================================================================
// Auth header propagation
// =============================================================================

describe('RestBarrierClient auth headers', () => {
  it('attaches a bearer token when configured', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ sealed: true, strategy: 'shamir', hardware_backed: false }),
    );

    const client = new RestClient('http://localhost', { token: 'abc123' });
    await client.barrier.status();

    const [, init] = fetchMock.mock.calls[0];
    const headers = init.headers as Record<string, string>;
    expect(headers['Authorization']).toBe('Bearer abc123');
  });

  it('attaches X-Tenant-ID when tenantID is configured', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ sealed: false, strategy: 'shamir', hardware_backed: false }),
    );

    const client = new RestClient('http://localhost', { tenantID: 'tenant-x' });
    await client.barrier.status();

    const [, init] = fetchMock.mock.calls[0];
    const headers = init.headers as Record<string, string>;
    expect(headers['X-Tenant-ID']).toBe('tenant-x');
  });
});

// =============================================================================
// Deprecated SealClient still works and warns once
// =============================================================================

describe('Deprecated SealClient alias', () => {
  it('routes legacy seal calls to the legacy /api/v1/seal endpoints', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        sealed: true,
        initialized: true,
        threshold: 2,
        total_shares: 3,
        shares_provided: 0,
        version: 1,
      }),
    );

    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const client = new RestClient('http://localhost');
    const status = await client.seal.status();
    warnSpy.mockRestore();

    expect(status.sealed).toBe(true);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/seal/status');
  });
});
