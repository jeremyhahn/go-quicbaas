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
import { createClient } from '@quicbaas/sdk';

import {
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  REFRESH_PATH,
  authStore,
  clearSessionEndReason,
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
    clearSessionEndReason();
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
    // No rotation in the body, so the credential we presented is spent: the
    // server revokes on every use. This test used to assert refresh-1 was
    // RETAINED, which meant the next renewal would replay a revoked token and
    // trip the stolen-token detection instead of failing cleanly.
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
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
      if (url.includes(REFRESH_PATH)) {
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
      if (url.includes(REFRESH_PATH)) {
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

  // -------------------------------------------------------------------------
  // go-quicbaas#256 — the refresh→401 storm.
  //
  // On the burn-in node the server refused the bearer it had JUST minted (the
  // validate verb was unregistered on a composed node and fell through to
  // mTLS). The interceptor treated that as a stale token and renewed, over and
  // over: refresh→200, validate→401, repeat, >1000 requests in about a minute
  // with no backoff and no terminal state, every view empty. The server half
  // is fixed, but a client that answers "renew again" to a credential the
  // server refused on the very next call will storm against ANY such server.
  // -------------------------------------------------------------------------
  it('ends the session instead of looping when a fresh bearer is refused', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    let refreshCalls = 0;
    let validateCalls = 0;
    const base = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes(REFRESH_PATH)) {
        refreshCalls++;
        // The server happily mints a NEW token every time — exactly what made
        // this loop self-sustaining rather than self-limiting.
        return jsonResponse(200, {
          access_token: jwtExpiringIn(900),
          refresh_token: `refresh-${refreshCalls + 1}`,
        });
      }
      validateCalls++;
      return jsonResponse(401, {
        error: 'qrdb/auth: no_peer_cert: no client certificate in TLS connection',
      });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    // Five request chains, as the SPA's repeated session checks would make.
    for (let i = 0; i < 5; i++) {
      const resp = await window.fetch('/api/v1/iam/validate', {
        headers: { Authorization: 'Bearer whatever' },
      });
      expect(resp.status).toBe(401);
    }

    // Exactly ONE renewal across all five chains: the first chain renews,
    // finds the fresh bearer refused, and ends the session. From then on
    // there is no refresh credential, so no further renewal is attempted.
    expect(refreshCalls).toBe(1);
    // 2 calls for the renewing chain (original + replay), 1 for each of the
    // four that follow. Bounded, and bounded by construction rather than by
    // a retry counter that a new call site could forget to consult.
    expect(validateCalls).toBe(6);
    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
    // The operator is told why rather than being bounced to login in silence.
    expect(authStore.endReason).not.toBe('');
  });

  it('ends the session once when concurrent 401s share one renewal', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    let refreshCalls = 0;
    const base = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes(REFRESH_PATH)) {
        refreshCalls++;
        return jsonResponse(200, { access_token: jwtExpiringIn(900), refresh_token: 'refresh-2' });
      }
      return jsonResponse(401, { error: 'no_peer_cert' });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const headers = { Authorization: 'Bearer whatever' };
    await Promise.all([
      window.fetch('/api/v1/iam/validate', { headers }),
      window.fetch('/api/v1/agents', { headers }),
      window.fetch('/api/v1/jobs', { headers }),
    ]);

    expect(refreshCalls).toBe(1);
    expect(authStore.status).toBe('unauthenticated');
  });

  // The terminal decision is made after TWO awaits (renewal, then replay), so
  // it must not be applied to whatever session happens to be current when it
  // finally lands. Guarding on "is a refresh token stored" got this wrong: a
  // fresh sign-in restores one, so a stale chain would clear the new session.
  it('does not end a NEW session when a stale chain resolves after re-login', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    let releaseReplay: () => void = () => {};
    const replayGate = new Promise<void>((resolve) => {
      releaseReplay = resolve;
    });
    let apiCalls = 0;
    const base = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes(REFRESH_PATH)) {
        return jsonResponse(200, { access_token: jwtExpiringIn(900), refresh_token: 'refresh-2' });
      }
      apiCalls++;
      // Hold the REPLAY open so the user can sign in while it is in flight.
      if (apiCalls === 2) {
        await replayGate;
      }
      return jsonResponse(401, { error: 'no_peer_cert' });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const inFlight = window.fetch('/api/v1/iam/validate', {
      headers: { Authorization: 'Bearer whatever' },
    });
    await vi.waitFor(() => {
      expect(apiCalls).toBe(2);
    });

    // A whole new session, established while the doomed replay is pending.
    setAuthenticated({ access_token: jwtExpiringIn(900), refresh_token: 'refresh-new' });
    releaseReplay();
    await inFlight;

    // The stale chain's verdict belonged to the session that is already gone.
    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-new');
  });

  // refreshAccessToken drops the stored refresh credential when the server
  // answers 2xx WITHOUT a replacement. A guard that keyed on that credential
  // then skipped the cleanup for the one case that most needs it, leaving the
  // operator on a shell whose every call 401s and no reason on screen.
  it('ends the session when the renewal returned no replacement credential', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    const base = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes(REFRESH_PATH)) {
        // 2xx, new access token, and no refresh_token field.
        return jsonResponse(200, { access_token: jwtExpiringIn(900) });
      }
      return jsonResponse(401, { error: 'no_peer_cert' });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/api/v1/iam/validate', {
      headers: { Authorization: 'Bearer whatever' },
    });

    expect(resp.status).toBe(401);
    expect(authStore.status).toBe('unauthenticated');
    expect(authStore.endReason).not.toBe('');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
  });

  it('leaves the session alone when the replay succeeds', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    let apiCalls = 0;
    const base = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes(REFRESH_PATH)) {
        return jsonResponse(200, { access_token: jwtExpiringIn(900), refresh_token: 'refresh-2' });
      }
      apiCalls++;
      // Genuinely stale token: the renewal is exactly the right answer.
      return apiCalls === 1
        ? jsonResponse(401, { error: 'Invalid or expired token' })
        : jsonResponse(200, { ok: true });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch('/api/v1/projects', {
      headers: { Authorization: 'Bearer stale' },
    });

    expect(resp.status).toBe(200);
    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).not.toBeNull();
  });

  it('never intercepts the refresh endpoint itself', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });
    const base = vi.fn(async () => jsonResponse(401, { error: 'nope' }));
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const resp = await window.fetch(REFRESH_PATH, { method: 'POST' });

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

  it('commits its verdict when THIS tab renews the credential mid-validate', async () => {
    const { validateSession, setAuthStatus, refreshAccessToken } = await import(
      '../src/stores/auth.svelte.ts'
    );

    // A genuine same-tab renewal, driven through refreshAccessToken rather than
    // simulated with a direct localStorage write. That distinction is the whole
    // point: a raw write is what ANOTHER tab does, and treating it as a renewal
    // is the model that let a replacement session inherit this one's caches. The
    // property under test is the other one — a renewal this tab performs must
    // not strand its own in-flight validation at 'pending', which is what a
    // byte-equality check did.
    const live = jwtExpiringIn(600);
    const renewed = jwtExpiringIn(900);
    window.localStorage.setItem(TOKEN_KEY, live);
    window.localStorage.setItem(REFRESH_TOKEN_KEY, 'refresh-1');
    setAuthStatus('pending');

    let releaseValidate: () => void = () => {};
    const validateGate = new Promise<void>((resolve) => {
      releaseValidate = resolve;
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input).includes(REFRESH_PATH)) {
          return jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' });
        }
        await validateGate;
        return jsonResponse(200, { valid: true });
      }),
    );

    const validating = validateSession();
    // The renewal lands while the validation is still outstanding.
    await refreshAccessToken();
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(renewed);

    releaseValidate();
    await validating;

    expect(authStore.status).toBe('authenticated');
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

// ===========================================================================
// Which server answers the renewal (go-quicbaas#221)
//
// Every test above stubs fetch and answers ANY url, so all of them passed
// while production logged the operator out every 15 minutes: the SPA was
// POSTing its refresh credential to /api/v1/auth/refresh, which on a composed
// node is the xkms module's endpoint redeeming from a different token store
// than the quicbaas ceremony that minted the credential. The server answered
// 401 "invalid refresh token" and the session was cleared silently.
//
// These assert the path against LITERALS rather than the exported constant.
// Reading the constant back would only prove the module agrees with itself;
// the literals are what pins the endpoint, so moving it again has to be a
// deliberate act with the server change to match.
// ===========================================================================

describe('renewal endpoint', () => {
  beforeEach(() => {
    window.localStorage.clear();
    setAuthStatus('pending');
    clearSessionEndReason();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    uninstallAuthFetch();
    vi.restoreAllMocks();
  });

  it('renews against quicbaas own IAM-namespaced endpoint, not the xkms path', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    const seen: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        seen.push(String(input));
        return jsonResponse(200, { access_token: jwtExpiringIn(900) });
      }),
    );

    await refreshAccessToken();

    expect(seen).toHaveLength(1);
    const url = new URL(seen[0]!);
    expect(url.pathname).toBe('/api/v1/iam/refresh');
    // The xkms module owns this path on a composed node and cannot redeem a
    // credential quicbaas minted.
    expect(url.pathname).not.toBe('/api/v1/auth/refresh');
  });

  it('exports the same path the interceptor refuses to intercept', () => {
    // One constant, two consumers. Two copies of the literal is how the
    // interceptor would come to recurse on the renewal request itself.
    expect(REFRESH_PATH).toBe('/api/v1/iam/refresh');
  });
});

// ===========================================================================
// The session-validate verb (go-quicbaas#256)
//
// The SPA's session check is a literal POST to this verb — the #221 note that
// "the SPA validates by making an authenticated call" was wrong, which is why
// the route was left on xkms's plane and the daemon rejected its own bearer.
// ===========================================================================
describe('session-validate endpoint', () => {
  const VALIDATE_ORIGIN = 'https://baas.test';

  beforeEach(() => {
    window.localStorage.clear();
    setAuthStatus('pending');
    clearSessionEndReason();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    uninstallAuthFetch();
    vi.restoreAllMocks();
  });

  it('validates against quicbaas own IAM-namespaced verb, not the xkms path', async () => {
    const seen: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        seen.push(String(input));
        return jsonResponse(200, { valid: true });
      }),
    );

    const client = await createClient({ transport: 'rest', baseURL: VALIDATE_ORIGIN });
    await expect(client.auth.authValidate('a-bearer')).resolves.toBe(true);

    expect(seen).toHaveLength(1);
    const url = new URL(seen[0]!);
    expect(url.pathname).toBe('/api/v1/iam/validate');
    // xkms registers GET (not POST) at the /api/v1/auth path on a composed
    // node, so this POST matched no route and answered 401 no_peer_cert.
    expect(url.pathname).not.toBe('/api/v1/auth/validate');
  });

  it('treats a 200 that says valid:false as an invalid session', async () => {
    // ValidateToken answers 200 {"valid":false} when the service rejects the
    // token (pkg/api/rest/iam_login.go). Reading only the status code would
    // report a dead session as live and leave the operator on a shell whose
    // every call 401s.
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(200, { valid: false })));
    const client = await createClient({ transport: 'rest', baseURL: VALIDATE_ORIGIN });
    await expect(client.auth.authValidate('a-bearer')).resolves.toBe(false);
  });

  it('reports 401 as an invalid session rather than throwing', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(401, { error: 'qrdb/auth: no_peer_cert' })),
    );
    const client = await createClient({ transport: 'rest', baseURL: VALIDATE_ORIGIN });
    await expect(client.auth.authValidate('a-bearer')).resolves.toBe(false);
  });

  it('revokes against quicbaas own IAM-namespaced logout verb', async () => {
    const seen: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        seen.push(String(input));
        return jsonResponse(200, { message: 'logged out successfully' });
      }),
    );

    const client = await createClient({ transport: 'rest', baseURL: VALIDATE_ORIGIN });
    await client.auth.logout('a-bearer');

    expect(seen).toHaveLength(1);
    // logout swallows every failure, so sending it to xkms's store made the
    // miss silent: sign-out looked clean and the bearer stayed live.
    expect(new URL(seen[0]!).pathname).toBe('/api/v1/iam/logout');
  });
});

// ===========================================================================
// The SDK's own traffic reaches the interceptor
//
// The stores' polling calls are made by the SDK client, not by hand-written
// fetches in the views. If the SDK held its own fetch reference (captured at
// module load, or a worker realm) the 401 interceptor would never see those
// requests and the reactive half of renewal would be dead for exactly the
// traffic that keeps a supervised run alive. This drives a REAL SDK client.
// ===========================================================================

describe('SDK-originated requests recover through the interceptor', () => {
  const ORIGIN = 'https://baas.test';

  beforeEach(() => {
    window.localStorage.clear();
    setAuthStatus('pending');
    clearSessionEndReason();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    uninstallAuthFetch();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renews once and replays an SDK call the server answered with 401', async () => {
    // A comfortably-valid token: the pre-emptive factory must NOT fire, so the
    // renewal observed here is the reactive 401 path and nothing else.
    const live = jwtExpiringIn(600);
    const renewed = jwtExpiringIn(900);
    setAuthenticated({ access_token: live, refresh_token: 'refresh-1' });

    let refreshCalls = 0;
    const apiCalls: string[] = [];
    const base = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/api/v1/iam/refresh')) {
        refreshCalls++;
        return jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' });
      }
      const authorization = new Headers(init?.headers ?? {}).get('Authorization') ?? '';
      apiCalls.push(authorization);
      // The server revoked the bearer early (admin action / key rotation):
      // still inside its exp, rejected anyway.
      return apiCalls.length === 1
        ? jsonResponse(401, { error: 'Invalid or expired token' })
        : jsonResponse(200, { jobs: [] });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const client = await createClient({
      transport: 'rest',
      baseURL: ORIGIN,
      token: async () => getValidAccessToken(),
    });

    await expect(client.orchestrator.listJobs('system')).resolves.toEqual([]);

    // Two API attempts: the 401 and its replay. Anything less means the
    // interceptor never saw the SDK's request.
    expect(apiCalls).toHaveLength(2);
    expect(apiCalls[0]).toBe(`Bearer ${live}`);
    expect(apiCalls[1]).toBe(`Bearer ${renewed}`);
    // Single-flight: one renewal, not one per attempt.
    expect(refreshCalls).toBe(1);
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-2');
    expect(authStore.status).toBe('authenticated');
  });

  it('coalesces concurrent SDK 401s into a single renewal', async () => {
    const live = jwtExpiringIn(600);
    const renewed = jwtExpiringIn(900);
    setAuthenticated({ access_token: live, refresh_token: 'refresh-1' });

    let refreshCalls = 0;
    const seenBearers: string[] = [];
    const base = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/api/v1/iam/refresh')) {
        refreshCalls++;
        return jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' });
      }
      const authorization = new Headers(init?.headers ?? {}).get('Authorization') ?? '';
      seenBearers.push(authorization);
      return authorization === `Bearer ${renewed}`
        ? jsonResponse(200, { jobs: [] })
        : jsonResponse(401, { error: 'Invalid or expired token' });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    const client = await createClient({
      transport: 'rest',
      baseURL: ORIGIN,
      token: async () => getValidAccessToken(),
    });

    // The dashboard's polling shape: several calls in flight at once, all of
    // which 401. One renewal must serve all of them.
    await Promise.all([
      client.orchestrator.listJobs('system'),
      client.orchestrator.listJobs('system'),
      client.orchestrator.listJobs('system'),
    ]);

    expect(refreshCalls).toBe(1);
    expect(seenBearers.filter((b) => b === `Bearer ${renewed}`)).toHaveLength(3);
  });
});

// ===========================================================================
// A failed renewal explains itself (go-quicbaas#221)
// ===========================================================================

describe('session-end reason', () => {
  beforeEach(() => {
    window.localStorage.clear();
    setAuthStatus('pending');
    clearSessionEndReason();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    uninstallAuthFetch();
    vi.restoreAllMocks();
  });

  it('records the server reason when the renewal is refused', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(401, {
          error: 'unauthorized',
          error_description: 'invalid or expired refresh token',
        }),
      ),
    );

    await refreshAccessToken();

    expect(authStore.status).toBe('unauthenticated');
    expect(authStore.endReason).toContain('invalid or expired refresh token');
    expect(authStore.endReason).toContain('401');
  });

  it('falls back to the status when the body explains nothing', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('gateway is down', { status: 502 })),
    );

    await refreshAccessToken();

    expect(authStore.endReason).toContain('502');
  });

  it('ends the session when a 2xx body cannot be read', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    // 200, but the body is not JSON. The server has already rotated, so the
    // credential in hand is dead — this is NOT the transient case and must not
    // be treated as one.
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('<html>proxy error</html>', { status: 200 })),
    );

    await expect(refreshAccessToken()).resolves.toBe('');
    expect(authStore.status).toBe('unauthenticated');
    expect(authStore.endReason).not.toBe('');
  });

  it('explains a session with no renewal credential at all', async () => {
    window.localStorage.setItem(TOKEN_KEY, jwtExpiringIn(-30));

    await refreshAccessToken();

    expect(authStore.endReason).not.toBe('');
  });

  it('says nothing after a deliberate sign-out', async () => {
    const { setUnauthenticated } = await import('../src/stores/auth.svelte.ts');
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-1' });

    setUnauthenticated();

    expect(authStore.status).toBe('unauthenticated');
    expect(authStore.endReason).toBe('');
  });

  it('clears the reason once the session is renewed', async () => {
    setAuthenticated({ access_token: jwtExpiringIn(-30), refresh_token: 'refresh-1' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(401, { error_description: 'nope' })),
    );
    await refreshAccessToken();
    expect(authStore.endReason).not.toBe('');

    // A later successful renewal must not leave the stale explanation behind
    // for the next time the operator lands on the login page.
    setAuthenticated({ access_token: jwtExpiringIn(600), refresh_token: 'refresh-2' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(200, { access_token: jwtExpiringIn(900) })),
    );
    await refreshAccessToken();

    expect(authStore.endReason).toBe('');
  });
});
