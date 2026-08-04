// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression tests for fetchJSON void / 204-handling (BUG 2).
//
// Bug: fetchJSON previously called resp.json() for every 2xx response,
// including 204 No Content and 200 responses with an empty body.
// resp.json() on an empty body throws "Unexpected end of JSON input",
// which surfaced as an unhandled error after the IAM AssignRole 204
// response — the assign dialog stayed open even though the server
// had already committed the change.
//
// Fix: all fetchJSON and fetchSSH success tails now delegate to the
// shared parseResponseBody<T> free function, which returns undefined
// for 204 No Content and for non-empty-body 2xx responses uses
// resp.text() + conditional JSON.parse so empty bodies never throw.
//
// These tests assert that void-typed SDK methods (and the underlying
// fetchJSON/fetchSSH) resolve cleanly under 204, 200-empty, and
// 200-with-JSON conditions across all previously-affected clients
// (Orchestrator, IAM, KMS, Secrets, KV, SSH).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

// ---------------------------------------------------------------------------
// Global fetch mock lifecycle
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// make204 builds a minimal fetch response-alike for 204 No Content.
// We use a plain object (not a real Response) so the mock works even in
// environments where the Response constructor restricts null bodies.
function make204(): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  return {
    ok: true,
    status: 204,
    json: async () => { throw new SyntaxError('no body'); },
    text: async () => '',
  };
}

// make200Empty returns a 200 with empty text body (some proxies strip the body).
function make200Empty(): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  return {
    ok: true,
    status: 200,
    json: async () => { throw new SyntaxError('Unexpected end of JSON input'); },
    text: async () => '',
  };
}

// make200JSON returns a 200 with a JSON body.
function make200JSON(payload: unknown): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  const body = JSON.stringify(payload);
  return {
    ok: true,
    status: 200,
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

// ---------------------------------------------------------------------------
// BUG 2 regression: disableAgent + enableAgent on 204
// ---------------------------------------------------------------------------

describe('fetchJSON — 204 No Content (void operations)', () => {
  it('disableAgent resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    // Must resolve without throwing "Unexpected end of JSON input".
    await expect(
      client.orchestrator.disableAgent({ agent_id: 'a1', tenant_id: 'system' }),
    ).resolves.not.toThrow();
  });

  it('enableAgent resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(
      client.orchestrator.enableAgent({ agent_id: 'a1', tenant_id: 'system' }),
    ).resolves.not.toThrow();
  });

  it('assignRoleToUser resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(
      client.iam.assignRoleToUser('system', 'u1', 'role-uuid'),
    ).resolves.not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// 200 with empty body (edge case some proxies produce)
// ---------------------------------------------------------------------------

describe('fetchJSON — 200 with empty body', () => {
  it('disableAgent resolves without throwing on a 200 empty-body response', async () => {
    fetchMock.mockResolvedValue(make200Empty());

    const client = new RestClient('http://localhost');
    // The old code called resp.json() which would throw; the fix uses text() first.
    await expect(
      client.orchestrator.disableAgent({ agent_id: 'a1', tenant_id: 'system' }),
    ).resolves.not.toThrow();
  });

  it('enableAgent resolves without throwing on a 200 empty-body response', async () => {
    fetchMock.mockResolvedValue(make200Empty());

    const client = new RestClient('http://localhost');
    await expect(
      client.orchestrator.enableAgent({ agent_id: 'a1', tenant_id: 'system' }),
    ).resolves.not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// 200 with JSON body still works correctly
// ---------------------------------------------------------------------------

describe('fetchJSON — 200 with JSON body (happy path not regressed)', () => {
  it('disableAgent returns parsed JSON on a 200 body response', async () => {
    const now = new Date().toISOString();
    fetchMock.mockResolvedValue(
      make200JSON({ agent_id: 'a1', disabled: true, disabled_at: now, timestamp: now }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.disableAgent({ agent_id: 'a1', tenant_id: 'system' });
    expect(result).toBeDefined();
    expect((result as { disabled?: boolean }).disabled).toBe(true);
  });

  it('enableAgent returns parsed JSON on a 200 body response', async () => {
    const now = new Date().toISOString();
    fetchMock.mockResolvedValue(
      make200JSON({ agent_id: 'a1', enabled: true, timestamp: now }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.enableAgent({ agent_id: 'a1', tenant_id: 'system' });
    expect(result).toBeDefined();
    expect((result as { enabled?: boolean }).enabled).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Previously-unfixed clients: KMS, Secrets, KV, SSH
// Each client had its own fetchJSON copy that ended with resp.json() and was
// vulnerable to empty-body throws. The shared parseResponseBody helper now
// covers all of them uniformly.
// ---------------------------------------------------------------------------

describe('KMS deleteKey — previously-unfixed fetchJSON client', () => {
  it('resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(client.kms.deleteKey('mykey')).resolves.not.toThrow();
  });

  it('resolves without throwing on a 200 empty-body response', async () => {
    fetchMock.mockResolvedValue(make200Empty());

    const client = new RestClient('http://localhost');
    await expect(client.kms.deleteKey('mykey')).resolves.not.toThrow();
  });

  it('returns parsed data on a 200 JSON response (rotateKey)', async () => {
    fetchMock.mockResolvedValue(
      make200JSON({ key_id: 'mykey', version: 2, timestamp: new Date().toISOString() }),
    );

    const client = new RestClient('http://localhost');
    const result = await client.kms.rotateKey('mykey');
    expect(result).toBeDefined();
    expect((result as { key_id?: string }).key_id).toBe('mykey');
  });
});

describe('Secrets deleteSecret — previously-unfixed fetchJSON client', () => {
  it('resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(client.secrets.deleteSecret('my/secret')).resolves.not.toThrow();
  });

  it('resolves without throwing on a 200 empty-body response', async () => {
    fetchMock.mockResolvedValue(make200Empty());

    const client = new RestClient('http://localhost');
    await expect(client.secrets.deleteSecret('my/secret')).resolves.not.toThrow();
  });
});

describe('KV kvPut / kvDelete — previously-unfixed fetchJSON client', () => {
  it('kvPut resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(client.data.kvPut('user', 'k1', { name: 'test' })).resolves.not.toThrow();
  });

  it('kvDelete resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(client.data.kvDelete('user', 'k1')).resolves.not.toThrow();
  });

  it('kvDelete resolves without throwing on a 200 empty-body response', async () => {
    fetchMock.mockResolvedValue(make200Empty());

    const client = new RestClient('http://localhost');
    await expect(client.data.kvDelete('user', 'k1')).resolves.not.toThrow();
  });
});

describe('SSH known-host add — previously-unfixed fetchSSH client', () => {
  it('resolves without throwing on a 204 response', async () => {
    fetchMock.mockResolvedValue(make204());

    const client = new RestClient('http://localhost');
    await expect(
      client.ssh.knownHosts.add({ fingerprint: 'SHA256:abc', host: 'host.example.com' }),
    ).resolves.not.toThrow();
  });

  it('resolves without throwing on a 200 empty-body response', async () => {
    fetchMock.mockResolvedValue(make200Empty());

    const client = new RestClient('http://localhost');
    await expect(
      client.ssh.knownHosts.add({ fingerprint: 'SHA256:abc', host: 'host.example.com' }),
    ).resolves.not.toThrow();
  });
});
