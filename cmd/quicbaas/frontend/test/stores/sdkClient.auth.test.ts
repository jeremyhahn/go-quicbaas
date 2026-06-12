// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * Regression coverage for the auth choke point.
 *
 * Bug: after a successful passkey login, orchestrator API calls were sent
 * WITHOUT the Authorization bearer token, so every orchestrator endpoint
 * returned 401. Root cause: getSDKClient() built the singleton REST client
 * with no `token` in its config, so the SDK's resolveToken() returned '' and
 * the Authorization header was never attached.
 *
 * Fix: getSDKClient() now passes a token *factory* — `() => readAuthToken()` —
 * so the SDK re-reads the live access token from the shared auth store on every
 * request. These tests assert:
 *   1. The choke point passes a token factory to createClient that resolves to
 *      the persisted access token.
 *   2. The factory returns '' when no token is stored (public-call safe).
 *   3. End-to-end through the real SDK REST client, an orchestrator request
 *      carries `Authorization: Bearer <token>` when a token is present and
 *      omits it when absent.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '@quicbaas/sdk';
import type { ClientConfig } from '@quicbaas/sdk';
import { getSDKClient, resetSDKClient } from '../../src/stores/sdkClient.ts';
import {
  TOKEN_KEY,
  setAuthenticated,
  setUnauthenticated,
} from '../../src/stores/auth.svelte.ts';

// Capture the config createClient is invoked with, while still returning a
// usable client object so getSDKClient resolves.
vi.mock('@quicbaas/sdk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@quicbaas/sdk')>();
  return {
    ...actual,
    createClient: vi.fn(actual.createClient),
    // Force the browser/REST path regardless of the jsdom runtime.
    isWailsRuntime: () => false,
  };
});

describe('getSDKClient auth choke point', () => {
  beforeEach(() => {
    resetSDKClient();
    setUnauthenticated();
    vi.mocked(createClient).mockClear();
  });

  afterEach(() => {
    resetSDKClient();
    setUnauthenticated();
  });

  it('passes a token factory that resolves to the persisted access token', async () => {
    setAuthenticated({ access_token: 'access-abc', refresh_token: 'refresh-xyz' });

    await getSDKClient();

    expect(createClient).toHaveBeenCalledTimes(1);
    const config = vi.mocked(createClient).mock.calls[0]![0] as ClientConfig;

    // It must be a factory, not a baked-in static string — the client is a
    // singleton created once, often before login.
    expect(typeof config.token).toBe('function');

    const resolved = await (config.token as () => Promise<string>)();
    expect(resolved).toBe('access-abc');
  });

  it('token factory resolves to empty string when no token is stored', async () => {
    // No setAuthenticated — public-call scenario (login/begin, invite GET).
    await getSDKClient();

    const config = vi.mocked(createClient).mock.calls[0]![0] as ClientConfig;
    const resolved = await (config.token as () => Promise<string>)();
    expect(resolved).toBe('');
  });

  it('factory re-reads the live token, so login AFTER client creation is honored', async () => {
    // Client is created while unauthenticated (the real-world ordering: the
    // login view fetches the client to run the handshake).
    await getSDKClient();
    const config = vi.mocked(createClient).mock.calls[0]![0] as ClientConfig;
    const factory = config.token as () => Promise<string>;

    expect(await factory()).toBe('');

    // Login happens later; the same factory must now see the new token.
    setAuthenticated({ access_token: 'late-token' });
    expect(await factory()).toBe('late-token');
  });
});

describe('orchestrator request carries Authorization end-to-end', () => {
  const ORIGIN = 'https://baas.test';
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    setUnauthenticated();
    fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ jobs: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    setUnauthenticated();
  });

  // Build a real SDK REST client with the same token-factory wiring
  // getSDKClient uses, so this exercises the production header path.
  function authedHeaders(): Record<string, string> {
    const init = fetchMock.mock.calls[0]![1] as RequestInit;
    return init.headers as Record<string, string>;
  }

  it('attaches Bearer token to GET /orchestrator/jobs when a token is stored', async () => {
    window.localStorage.setItem(TOKEN_KEY, 'orch-token-123');

    const client = await createClient({
      transport: 'rest',
      baseURL: ORIGIN,
      token: async () => window.localStorage.getItem(TOKEN_KEY) ?? '',
    });

    await client.orchestrator.listJobs('system');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const url = fetchMock.mock.calls[0]![0] as string;
    expect(url).toContain('/api/v1/orchestrator/jobs');
    expect(authedHeaders()['Authorization']).toBe('Bearer orch-token-123');
  });

  it('omits Authorization when no token is stored', async () => {
    const client = await createClient({
      transport: 'rest',
      baseURL: ORIGIN,
      token: async () => window.localStorage.getItem(TOKEN_KEY) ?? '',
    });

    await client.orchestrator.healthCheck();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(authedHeaders()['Authorization']).toBeUndefined();
  });
});
