// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestOrchestratorClient.rotateSessionAccount.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { RotateSessionAccountResponse } from '../src/types.js';
import { RestClient } from '../src/rest.js';

// mockOK returns a minimal fetch Response-alike that supports both .json()
// and .text() so tests remain compatible with fetchJSON implementations that
// use resp.text() (post BUG-2 fix) and those that use resp.json().
function mockOK(payload: unknown): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  const body = JSON.stringify(payload);
  return { ok: true, status: 200, json: async () => JSON.parse(body) as unknown, text: async () => body };
}

function mockError(status: number, payload: unknown): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  const body = JSON.stringify(payload);
  return { ok: false, status, json: async () => JSON.parse(body) as unknown, text: async () => body };
}

describe('RestOrchestratorClient.rotateSessionAccount', () => {
  const BASE = 'http://test-server';
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;

  const response: RotateSessionAccountResponse = {
    session_id: 'sess-1',
    previous_account_id: 'acct-a',
    account_id: 'acct-b',
    strategy: 'least_usage',
    mode: 'resume',
  };

  beforeEach(() => {
    client = new RestClient(BASE);
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('POSTs to /sessions/:sessionID/rotate-account with the session ID in the path', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(response));

    await client.orchestrator.rotateSessionAccount({ session_id: 'sess-1' });

    expect(fetchMock).toHaveBeenCalledOnce();
    const [calledUrl, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const url = new URL(calledUrl);
    expect(url.pathname).toBe('/api/v1/orchestrator/sessions/sess-1/rotate-account');
    expect(init.method).toBe('POST');
  });

  it('omits session_id from the JSON body — the path segment is authoritative', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(response));

    await client.orchestrator.rotateSessionAccount({ session_id: 'sess-1', reason: 'rebalance' });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body.session_id).toBeUndefined();
    expect(body.reason).toBe('rebalance');
  });

  it('sends an empty body for a plain rotation with no reason or hold', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(response));

    await client.orchestrator.rotateSessionAccount({ session_id: 'sess-1' });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body).toEqual({});
  });

  it('sends hold_outgoing and available_at when a hold is requested', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(response));

    await client.orchestrator.rotateSessionAccount({
      session_id: 'sess-1',
      hold_outgoing: true,
      available_at: '2026-08-09T12:00:00Z',
    });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body.hold_outgoing).toBe(true);
    expect(body.available_at).toBe('2026-08-09T12:00:00Z');
  });

  it('returns the previous and replacement account IDs plus the mode', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(response));

    const result = await client.orchestrator.rotateSessionAccount({ session_id: 'sess-1' });

    expect(result.previous_account_id).toBe('acct-a');
    expect(result.account_id).toBe('acct-b');
    expect(result.mode).toBe('resume');
  });

  it('throws at runtime when session_id is empty (defense in depth)', async () => {
    await expect(
      // Cast needed to simulate a caller that bypasses TypeScript (e.g. JS interop).
      client.orchestrator.rotateSessionAccount({ session_id: '' } as never),
    ).rejects.toThrow('rotateSessionAccount: session_id is required');

    // fetch must never have been called when session_id is absent.
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('propagates a SessionNotRotatable-style error response', async () => {
    fetchMock.mockResolvedValueOnce(
      mockError(409, { error: 'session is not rotatable: pinned to a single account' }),
    );

    await expect(
      client.orchestrator.rotateSessionAccount({ session_id: 'sess-pinned' }),
    ).rejects.toThrow();
  });
});
