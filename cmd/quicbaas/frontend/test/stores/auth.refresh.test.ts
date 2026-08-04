/**
 * auth.refresh.test.ts
 *
 * Regression coverage for bug #20: a long-lived tab whose 15-minute access
 * token expires mid-session must transparently obtain a fresh one from the
 * stored refresh token instead of silently 401-ing. getValidAccessToken() is
 * the SDK token-factory source of truth and must:
 *   • return a comfortably-valid token unchanged (no needless refresh),
 *   • pre-emptively refresh a token inside the skew window and persist the new
 *     access token (keeping the reused refresh token),
 *   • fail the session closed when the server rejects the refresh token,
 *   • coalesce concurrent callers into a single refresh request.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  setAuthenticated,
  setUnauthenticated,
  authStore,
  getValidAccessToken,
} from '../../src/stores/auth.svelte.ts';

// makeJWT builds an unsigned JWT whose payload carries the given exp (seconds
// since epoch). Only the payload segment is meaningful to secondsUntilExpiry.
function makeJWT(expEpochSeconds: number): string {
  const b64url = (o: unknown): string =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  return `${b64url({ alg: 'none' })}.${b64url({ exp: expEpochSeconds })}.sig`;
}

const nowSec = (): number => Math.floor(Date.now() / 1000);

describe('getValidAccessToken — proactive refresh (bug #20)', () => {
  beforeEach(() => {
    setUnauthenticated();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    setUnauthenticated();
    vi.restoreAllMocks();
  });

  it('returns a comfortably-valid token without refreshing', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    setAuthenticated({ access_token: makeJWT(nowSec() + 3600), refresh_token: 'r' });

    const token = await getValidAccessToken();

    expect(token).toContain('.'); // the JWT we set
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('refreshes a near-expiry token and persists the new access token', async () => {
    const fresh = makeJWT(nowSec() + 3600);
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ access_token: fresh, refresh_token: '' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    // Token already inside the 60s skew window.
    setAuthenticated({ access_token: makeJWT(nowSec() + 10), refresh_token: 'refresh-keep' });

    const token = await getValidAccessToken();

    expect(token).toBe(fresh);
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(fresh);
    // The refresh token is reused, not replaced.
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-keep');
    expect(fetchSpy).toHaveBeenCalledOnce();
    expect(authStore.status).toBe('authenticated');
  });

  it('fails the session closed when the refresh token is rejected', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('nope', { status: 401 }));
    setAuthenticated({ access_token: makeJWT(nowSec() + 5), refresh_token: 'stale' });

    const token = await getValidAccessToken();

    expect(token).toBe('');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(authStore.status).toBe('unauthenticated');
  });

  it('does not attempt a refresh when no refresh token is stored', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    // Near-expiry access token but no refresh token — nothing to refresh with.
    window.localStorage.setItem(TOKEN_KEY, makeJWT(nowSec() + 5));

    const token = await getValidAccessToken();

    expect(token).toContain('.');
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('coalesces concurrent refreshes into a single request', async () => {
    const fresh = makeJWT(nowSec() + 3600);
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(
      () =>
        new Promise((resolve) =>
          setTimeout(
            () =>
              resolve(
                new Response(JSON.stringify({ access_token: fresh }), {
                  status: 200,
                  headers: { 'Content-Type': 'application/json' },
                }),
              ),
            10,
          ),
        ),
    );
    setAuthenticated({ access_token: makeJWT(nowSec() + 5), refresh_token: 'r' });

    const [a, b, c] = await Promise.all([
      getValidAccessToken(),
      getValidAccessToken(),
      getValidAccessToken(),
    ]);

    expect(a).toBe(fresh);
    expect(b).toBe(fresh);
    expect(c).toBe(fresh);
    expect(fetchSpy).toHaveBeenCalledOnce();
  });
});
