// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestOrchestratorClient.listSessions tenant isolation.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { ListSessionsResponse } from '../src/types.js';
import { RestClient } from '../src/rest.js';

// mockOK returns a minimal fetch Response-alike that supports both .json()
// and .text() so tests remain compatible with fetchJSON implementations that
// use resp.text() (post BUG-2 fix) and those that use resp.json().
function mockOK(payload: unknown): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  const body = JSON.stringify(payload);
  return { ok: true, status: 200, json: async () => JSON.parse(body) as unknown, text: async () => body };
}

describe('RestOrchestratorClient.listSessions', () => {
  const BASE = 'http://test-server';
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;

  const emptyResponse: ListSessionsResponse = { sessions: [] };

  beforeEach(() => {
    client = new RestClient(BASE);
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('serializes tenant_id into the query string', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(emptyResponse));

    await client.orchestrator.listSessions({ tenantID: 'acme', backend: 'ollama' });

    expect(fetchMock).toHaveBeenCalledOnce();
    const calledUrl = fetchMock.mock.calls[0][0] as string;
    const params = new URL(calledUrl).searchParams;
    expect(params.get('tenant_id')).toBe('acme');
    expect(params.get('backend')).toBe('ollama');
  });

  it('URL is exactly /api/v1/orchestrator/sessions with tenant_id and optional filters', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(emptyResponse));

    await client.orchestrator.listSessions({ tenantID: 'acme', backend: 'ollama' });

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    const url = new URL(calledUrl);
    expect(url.pathname).toBe('/api/v1/orchestrator/sessions');
    expect(url.searchParams.has('tenant_id')).toBe(true);
    expect(url.searchParams.get('tenant_id')).toBe('acme');
    expect(url.searchParams.get('backend')).toBe('ollama');
  });

  it('includes status filter when provided', async () => {
    fetchMock.mockResolvedValueOnce(mockOK(emptyResponse));

    await client.orchestrator.listSessions({ tenantID: 'acme', status: 'running' });

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    const params = new URL(calledUrl).searchParams;
    expect(params.get('tenant_id')).toBe('acme');
    expect(params.get('status')).toBe('running');
  });

  it('throws at runtime when tenantID is empty string (defense in depth)', async () => {
    await expect(
      // Cast needed to simulate a caller that bypasses TypeScript (e.g. JS interop).
      client.orchestrator.listSessions({ tenantID: '' } as never),
    ).rejects.toThrow('listSessions: tenantID is required');

    // fetch must never have been called when tenantID is absent.
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
