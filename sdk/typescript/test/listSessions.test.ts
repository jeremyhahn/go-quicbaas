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
  // ---------------------------------------------------------------------------
  // What the transport actually returns (go-quicbaas#864)
  // ---------------------------------------------------------------------------

  /**
   * THE SHAPE, PINNED. `ListSessionsResponse.sessions` is typed
   * `SessionStatusResponse[]`, which names the lifecycle field `status` and the
   * activity instant `last_active`. This client does no translation, and the
   * service writes `orchestration.SessionInfo` verbatim — `state` and
   * `last_activity`. So the declared type is not what a caller receives.
   *
   * Two consumers believed the type instead of the wire and counted `status` over
   * records that had none, and their "Active Sessions" tiles read 0 while live
   * sessions were running (go-quicbaas#864).
   *
   * #856 HAS NOW LANDED (PR #859, merge bca1a46d), so the rename exists at this
   * seam and this test asserts the new truth: a caller gets `status` and
   * `last_active`, and the wire's own names are gone by the time it sees them.
   *
   * The previous revision of this test pinned the opposite and was written to
   * FAIL when #856 landed — which is exactly what it did. That failure was the
   * signal, not a regression; it is recorded here rather than silently flipped
   * so the next reader knows the rename is deliberate and where it happens.
   */
  it('normalises the service\'s field names — `status`, not `state`', async () => {
    fetchMock.mockResolvedValueOnce(
      mockOK({
        sessions: [
          {
            session_id: 'sess-1',
            project_id: 'proj-1',
            backend: 'claude-code',
            model: 'sonnet',
            state: 'active',
            created_at: 1_700_000_000_000_000_000,
            last_activity: 1_700_000_000_500_000_000,
          },
        ],
        count: 1,
      }),
    );

    const resp = await client.orchestrator.listSessions({ tenantID: 'acme' });

    expect(resp.sessions).toHaveLength(1);
    const session = resp.sessions[0] as unknown as Record<string, unknown>;
    // The DECLARED names are what a caller gets, because the SDK renames at
    // this seam now.
    expect(session['status']).toBe('active');
    expect(session['last_active']).toBe(1_700_000_000_500_000_000);
    // The wire's own names do not survive the mapping.
    expect(session['state']).toBeUndefined();
    expect(session['last_activity']).toBeUndefined();
  });

  it('normalises a missing count to the number of sessions returned', async () => {
    fetchMock.mockResolvedValueOnce(
      mockOK({ sessions: [{ session_id: 'a', state: 'active' }, { session_id: 'b', state: 'closed' }] }),
    );

    const resp = await client.orchestrator.listSessions({ tenantID: 'acme' });

    expect(resp.count).toBe(2);
  });
});
