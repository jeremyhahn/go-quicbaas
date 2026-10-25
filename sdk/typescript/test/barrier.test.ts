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
import { APIError, McpError, SOPINRequiresTLSError } from '../src/errors';
import { createBarrierClient } from '../src/factory';
import { McpClient } from '../src/mcp';
import { jsonReply, scriptServer, toolCalls, toolText } from './mcp_server';

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
    await client.barrier.initialize({ secret: new TextEncoder().encode('topsecret') });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/barrier/initialize');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ secret: 'dG9wc2VjcmV0' });
  });

  it('unseals the barrier with a passphrase', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.unseal({ secret: new TextEncoder().encode('topsecret') });

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
    expect(JSON.parse(init.body as string)).toEqual({});
  });

  it('seal sends the SO PIN as so_pin over https', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('https://node.example');
    await client.barrier.seal({ so_pin: 'so-pin-1234' });

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ so_pin: 'so-pin-1234' });
  });

  it('seal refuses to send an SO PIN over plaintext http, before any request', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    for (const base of ['http://node.example', 'http://localhost', 'http://127.0.0.1:8080']) {
      const client = new RestClient(base);
      const err = await client.barrier.seal({ so_pin: 'so-pin-1234' }).catch((e: unknown) => e);
      expect(err).toBeInstanceOf(SOPINRequiresTLSError);
      expect((err as SOPINRequiresTLSError).transport).toBe('rest');
      expect((err as Error).message).not.toContain('so-pin-1234');
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('seal with an empty SO PIN still works over http', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.seal({ so_pin: '' });

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('seal surfaces the server refusing a wrong SO PIN', async () => {
    fetchMock.mockResolvedValue(errorResponse(403, 'SO PIN invalid'));

    const client = new RestClient('https://node.example');
    await expect(client.barrier.seal({ so_pin: 'wrong' })).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// enableEncryption — go-qrdb wire mount POST /v1/barrier/enable-encryption
// =============================================================================

describe('RestBarrierClient.enableEncryption', () => {
  it('posts the canonical request and returns the migration acknowledgement', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ status: 'migration_started', scope: 'tenant' }));

    const client = new RestClient('http://localhost');
    const resp = await client.barrier.enableEncryption({
      tenant_id: 't1',
      strategy: 'passphrase',
      mechanism: 'inplace',
      availability: 'quiesced',
      passphrase: new TextEncoder().encode('topsecret'),
    });

    expect(resp).toEqual({ status: 'migration_started', scope: 'tenant' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/v1/barrier/enable-encryption');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      tenant_id: 't1',
      strategy: 'passphrase',
      mechanism: 'inplace',
      availability: 'quiesced',
      passphrase: 'dG9wc2VjcmV0',
    });
  });

  it('sends no passphrase for a Shamir target', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ status: 'migration_started', scope: 'system' }));

    const client = new RestClient('http://localhost');
    await client.barrier.enableEncryption({ strategy: 'shamir', shamir_threshold: 2, shamir_total: 3 });

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({
      strategy: 'shamir',
      shamir_threshold: 2,
      shamir_total: 3,
    });
  });

  it('throws APIError when the server refuses the migration', async () => {
    fetchMock.mockResolvedValue(errorResponse(400, 'strategy is required'));

    const client = new RestClient('http://localhost');
    await expect(client.barrier.enableEncryption({ strategy: '' })).rejects.toBeInstanceOf(APIError);
  });

  it.each([
    ['grpc-web', { baseURL: 'http://localhost' }],
  ] as const)('is refused by the %s stub transport', async (transport, config) => {
    const barrier = await createBarrierClient(transport, config);
    await expect(barrier.enableEncryption({ strategy: 'shamir' })).rejects.toThrow(`${transport} transport`);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

// =============================================================================
// Shamir strategy — initialize, unseal, list, delete, verify
// =============================================================================

describe('RestBarrierClient credential bytes', () => {
  it('base64-encodes a credential that is not valid UTF-8', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.barrier.unseal({ secret: new Uint8Array([0x00, 0xff, 0xfe, 0x80]) });

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ secret: 'AP/+gA==' });
  });

  it('base64-encodes the tenant init passphrase and leaves the other fields alone', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.tenantBarrier.tenantBarrierInit('t1', {
      strategy: 'passphrase',
      passphrase: new TextEncoder().encode('topsecret'),
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/tenants/t1/barrier/init');
    expect(JSON.parse(init.body as string)).toEqual({
      strategy: 'passphrase',
      passphrase: 'dG9wc2VjcmV0',
    });
  });

  it('omits the tenant init passphrase when none is given', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.tenantBarrier.tenantBarrierInit('t1', { strategy: 'none' });

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ strategy: 'none' });
  });

  it('base64-encodes the tenant unseal key', async () => {
    fetchMock.mockResolvedValue(noContentResponse());

    const client = new RestClient('http://localhost');
    await client.tenantBarrier.tenantBarrierUnseal('t1', new Uint8Array([0x00, 0xff, 0xfe, 0x80]));

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/tenants/t1/barrier/unseal');
    expect(JSON.parse(init.body as string)).toEqual({ key: 'AP/+gA==' });
  });
});

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
      secret: new TextEncoder().encode('master'),
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

});

// =============================================================================
// Rekey, recovery, root token
// =============================================================================

describe('RestBarrierClient operator flows', () => {
  it('generates recovery keys with the SO PIN and policy over https', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ threshold: 2, total: 3, share_count: 3 }));

    const client = new RestClient('https://node.example');
    const result = await client.barrier.generateRecoveryKeys({
      threshold: 2,
      policy: 'designated',
      so_pin: 'so-pin-1234',
    });

    expect(result).toEqual({ threshold: 2, total: 3, share_count: 3 });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://node.example/api/v1/barrier/recovery/generate');
    expect(JSON.parse(init.body as string)).toEqual({ threshold: 2, policy: 'designated', so_pin: 'so-pin-1234' });
  });

  it('refuses to send a recovery-generation SO PIN over plaintext http, before any request', async () => {
    const err = await new RestClient('http://localhost')
      .barrier.generateRecoveryKeys({ threshold: 2, so_pin: 'so-pin-1234' })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(SOPINRequiresTLSError);
    expect((err as SOPINRequiresTLSError).transport).toBe('rest');
    expect(fetchMock).not.toHaveBeenCalled();
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
// McpBarrierClient.seal forwards the SO PIN as the so_pin tool argument
// =============================================================================

describe('McpBarrierClient.seal', () => {
  const endpoint = 'http://node.example:8081/mcp';
  const tlsEndpoint = 'https://node.example:8081/mcp';

  it('calls qrdb_barrier_seal with the SO PIN over https', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { status: 'sealed' })));

    await new McpClient(tlsEndpoint).barrier.seal({ so_pin: 'so-pin-1234' });

    const calls = toolCalls(fetchMock);
    expect(calls).toHaveLength(1);
    expect(calls[0].params?.name).toBe('qrdb_barrier_seal');
    expect(calls[0].params?.arguments).toEqual({ so_pin: 'so-pin-1234' });
  });

  it('sends no so_pin when none is given', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { status: 'sealed' })));

    await new McpClient(endpoint).barrier.seal();

    expect(toolCalls(fetchMock)[0].params?.arguments).toEqual({});
  });

  it('sends no so_pin for an empty PIN over http', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { status: 'sealed' })));

    await new McpClient(endpoint).barrier.seal({ so_pin: '' });

    expect(toolCalls(fetchMock)[0].params?.arguments).toEqual({});
  });

  it('refuses to send an SO PIN over plaintext http, before any request', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { status: 'sealed' })));

    for (const url of [endpoint, 'http://localhost:8081/mcp']) {
      const err = await new McpClient(url).barrier.seal({ so_pin: 'so-pin-1234' }).catch((e: unknown) => e);
      expect(err).toBeInstanceOf(SOPINRequiresTLSError);
      expect((err as SOPINRequiresTLSError).transport).toBe('mcp');
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('surfaces the server refusing a wrong SO PIN', async () => {
    scriptServer(fetchMock, (msg) =>
      jsonReply({ jsonrpc: '2.0', id: msg.id, result: { isError: true, content: [{ type: 'text', text: 'SO PIN invalid' }] } }),
    );

    await expect(new McpClient(tlsEndpoint).barrier.seal({ so_pin: 'wrong' })).rejects.toBeInstanceOf(McpError);
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
