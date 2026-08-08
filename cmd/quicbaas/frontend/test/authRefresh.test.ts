// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
 * Session-renewal tests.
 *
 * The bug these guard against: the operator signed in, browsed, and 15 minutes
 * later every page died with `{"error":"Invalid or expired token"}`. Access
 * tokens are deliberately short-lived, so the SPA must renew them — both
 * pre-emptively (token factory) and reactively (401 → refresh → replay).
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import {
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  authStore,
  getValidAccessToken,
  refreshAccessToken,
  setAuthenticated,
  setAuthStatus,
} from '../src/stores/auth.svelte.ts';
import { installAuthFetch, uninstallAuthFetch } from '../src/stores/authFetch.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build an unsigned JWT whose exp is `secondsFromNow` in the future. */
function jwtExpiringIn(secondsFromNow: number): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({ sub: 'user-1', exp: Math.floor(Date.now() / 1000) + secondsFromNow }),
  );
  return `${header}.${payload}.sig`;
}

/** A JSON Response with the given status/body. */
function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('SPA session renewal', () => {
  beforeEach(() => {
    window.localStorage.clear();
    setAuthStatus('pending');
    vi.restoreAllMocks();
  });

  afterEach(() => {
    uninstallAuthFetch();
    vi.restoreAllMocks();
  });

  // -------------------------------------------------------------------------
  // Storage
  // -------------------------------------------------------------------------

  it('persists both tokens returned by a login', () => {
    setAuthenticated({ access_token: 'access-1', refresh_token: 'refresh-1' });

    expect(window.localStorage.getItem(TOKEN_KEY)).toBe('access-1');
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-1');
    expect(authStore.status).toBe('authenticated');
  });

  // -------------------------------------------------------------------------
  // Pre-emptive refresh (token factory)
  // -------------------------------------------------------------------------

  it('uses a comfortably-valid access token without a round trip', async () => {
    const live = jwtExpiringIn(600);
    setAuthenticated({ access_token: live, refresh_token: 'refresh-1' });
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    await expect(getValidAccessToken()).resolves.toBe(live);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('renews an access token that has crossed the expiry boundary', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    const renewed = jwtExpiringIn(900);
    const fetchMock = vi.fn(async () =>
      jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' }),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(getValidAccessToken()).resolves.toBe(renewed);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(renewed);
    // The rotated credential MUST be stored: the server revoked refresh-1, so
    // keeping it would strand the session at the next renewal.
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-2');
  });

  it('renews a token that is inside the skew window but not yet expired', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(30), refresh_token: 'refresh-1' });
    const renewed = jwtExpiringIn(900);
    const fetchMock = vi.fn(async () => jsonResponse(200, { access_token: renewed }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(getValidAccessToken()).resolves.toBe(renewed);
    // No rotation returned: the stored credential is retained.
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-1');
  });

  it('coalesces concurrent renewals into a single request', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    const renewed = jwtExpiringIn(900);
    let resolveFetch: (r: Response) => void = () => {};
    const gate = new Promise<Response>((resolve) => {
      resolveFetch = resolve;
    });
    const fetchMock = vi.fn(async () => gate);
    vi.stubGlobal('fetch', fetchMock);

    const inflight = Promise.all([
      getValidAccessToken(),
      getValidAccessToken(),
      getValidAccessToken(),
    ]);
    resolveFetch(jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' }));

    await expect(inflight).resolves.toEqual([renewed, renewed, renewed]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('ends the session when the server rejects the refresh credential', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(401, { error: 'invalid or expired refresh token' })),
    );

    await expect(getValidAccessToken()).resolves.toBe('');
    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
  });

  it('keeps the session on a transient network failure', async () => {
    const stale = jwtExpiringIn(-30);
    setAuthenticated({ access_token: stale, refresh_token: 'refresh-1' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('network down');
      }),
    );

    // The existing token is returned so the next request retries, rather than
    // logging the operator out because their wifi blipped.
    await expect(refreshAccessToken()).resolves.toBe(stale);
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-1');
  });

  it('does not attempt a renewal without a stored refresh credential', async () => {
    window.localStorage.setItem(TOKEN_KEY, jwtExpiringIn(-30));
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    // Nothing to renew with: the token is returned as-is and the server's 401
    // drives the route guard.
    await expect(getValidAccessToken()).resolves.not.toBe('');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // Reactive recovery (401 interceptor)
  // -------------------------------------------------------------------------

  it('renews and replays a request that the server answered with 401', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    const renewed = jwtExpiringIn(900);

    const calls: Array<{ url: string; authorization: string }> = [];
    const base = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/api/v1/auth/refresh')) {
        return jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' });
      }
      const authorization = new Headers(init?.headers ?? {}).get('Authorization') ?? '';
      calls.push({ url, authorization });
      return calls.length === 1
        ? jsonResponse(401, { error: 'Invalid or expired token' })
        : jsonResponse(200, { projects: [] });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/api/v1/projects', {
      headers: { Authorization: 'Bearer stale' },
    });

    expect(resp.status).toBe(200);
    expect(calls).toHaveLength(2);
    expect(calls[0]?.authorization).toBe('Bearer stale');
    // The replay carries the RENEWED bearer, not the stale one.
    expect(calls[1]?.authorization).toBe(`Bearer ${renewed}`);
  });

  it('replays at most once — a second 401 is returned to the caller', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    let apiCalls = 0;
    const base = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/v1/auth/refresh')) {
        return jsonResponse(200, { access_token: jwtExpiringIn(900) });
      }
      apiCalls++;
      return jsonResponse(401, { error: 'Invalid or expired token' });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/api/v1/projects', {
      headers: { Authorization: 'Bearer stale' },
    });

    expect(resp.status).toBe(401);
    expect(apiCalls).toBe(2);
  });

  it('never intercepts the refresh endpoint itself', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    const base = vi.fn(async () => jsonResponse(401, { error: 'nope' }));
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/api/v1/auth/refresh', { method: 'POST' });

    expect(resp.status).toBe(401);
    // Exactly one call: no recursive renewal attempt.
    expect(base).toHaveBeenCalledTimes(1);
  });

  it('passes through a 401 when there is no session to renew', async () => {
    const base = vi.fn(async () => jsonResponse(401, { error: 'unauthenticated' }));
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/api/v1/projects');

    expect(resp.status).toBe(401);
    expect(base).toHaveBeenCalledTimes(1);
  });

  it('leaves non-API requests alone', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    const base = vi.fn(async () => jsonResponse(401, { error: 'nope' }));
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/assets/app.css');

    expect(resp.status).toBe(401);
    expect(base).toHaveBeenCalledTimes(1);
  });

  // -------------------------------------------------------------------------
  // Session-generation guard
  // -------------------------------------------------------------------------

  it('does not resurrect a session that was ended while validation was in flight', async () => {
    const { validateSession, setUnauthenticated } = await import('../src/stores/auth.svelte.ts');

    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    // An always-affirmative server: only the generation guard can prevent the
    // stale verdict from being committed.
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(200, { valid: true })),
    );

    const inFlight = validateSession();
    // The user signs out while the round trip is outstanding.
    setUnauthenticated();
    await inFlight;

    expect(authStore.status).toBe('unauthenticated');
  });

  it('installs only once', async () => {
    const base = vi.fn(async () => jsonResponse(200, {}));
    vi.stubGlobal('fetch', base);
    installAuthFetch();
    const wrapped = window.fetch;
    installAuthFetch();

    expect(window.fetch).toBe(wrapped);
  });
});
