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
 * Regression coverage for the "UI login loop" investigation (2026-09-25).
 *
 * Symptom: the owner signed in with a hardware key (xKey) and every
 * subsequent orchestrator/scheduler request went out with NO Authorization
 * header at all — not a rejected token, an ABSENT one — even though the
 * SDK's token factory and per-client authHeaders() wiring were provably
 * correct in isolation (see sdkClient.auth.test.ts and authRefresh.test.ts,
 * both of which already pass on this HEAD).
 *
 * Root cause: a SIBLING browser tab (or window) that was already open on a
 * dead session — refresh token already spent, or revoked — independently
 * renews on its own polling cycle. auth.svelte.ts's session bookkeeping
 * (`generation`) is per-tab, but localStorage is shared across every tab on
 * the origin. Two places trusted "my own generation hasn't moved" as proof
 * that nothing else had happened to the credential in storage, which is
 * true only within one tab:
 *
 *   1. persistAccessToken (commitRenewal's writer) would overwrite storage
 *      with the dead tab's rotated bearer even after a sibling tab had
 *      since written a brand-new login — clobbering the live session with
 *      one for a session nobody is using.
 *   2. endSessionIfCurrent (auth.svelte.ts's performRenewal, and
 *      authFetch.ts's 401-replay terminal state) would then unconditionally
 *      wipe TOKEN_KEY/REFRESH_TOKEN_KEY from shared storage on the dead
 *      tab's own renewal failure — even though what was actually sitting in
 *      storage by then was a different, live session that had nothing to do
 *      with the renewal that failed.
 *
 * Either one alone reproduces the reported symptom: the operator's freshly
 * written token disappears (or is overwritten) out from under them, minutes
 * or even seconds after a successful sign-in, with no further action in
 * their own tab to explain it.
 *
 * Fix: both call sites now check the LIVE `generationCredential` — the
 * credential THIS tab itself last wrote, updated on every write this tab
 * makes — against what is actually in storage at decision time, not a value
 * frozen before the round trip. That distinguishes "my own renewal rotated
 * the credential" (generationCredential moves WITH it, so they still agree)
 * from "someone else replaced it" (generationCredential stays put, storage
 * does not, so the write/clear is refused). See persistAccessToken's
 * `requireCurrentCredential` parameter and endSessionIfCurrent in
 * src/stores/auth.svelte.ts.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

/** Build an unsigned JWT whose exp is `secondsFromNow` in the future. */
function jwtExpiringIn(secondsFromNow: number): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({ sub: 'user-1', exp: Math.floor(Date.now() / 1000) + secondsFromNow }),
  );
  return `${header}.${payload}.sig`;
}

/**
 * Install a mock `navigator.locks` that behaves like the real Web Locks API
 * for the one thing these tests need: a NAMED exclusive lock is genuinely
 * shared across every `import()` of auth.svelte.ts in this test (each a
 * separate module instance, the same way two browser tabs are), serializing
 * callers in the order they requested the lock. Returns the underlying
 * request spy so a test can assert how many times the lock was taken.
 */
function installMockLocks(): ReturnType<typeof vi.fn> {
  let chain: Promise<unknown> = Promise.resolve();
  const request = vi.fn((_name: string, fn: () => Promise<unknown>) => {
    const run = chain.then(() => fn());
    // A holder that throws must not wedge the queue for whoever is next.
    chain = run.catch(() => undefined);
    return run;
  });
  Object.defineProperty(globalThis.navigator, 'locks', {
    value: { request },
    configurable: true,
  });
  return request;
}

/** A JSON Response with the given status/body. */
function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('cross-tab session isolation', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('a sibling tab renewing a dead session does not wipe a fresh login written in between', async () => {
    // Tab B: an already-open tab holding a dead refresh token.
    vi.resetModules();
    const tabB = await import('../src/stores/auth.svelte.ts');
    const bOldLive = jwtExpiringIn(600);
    tabB.setAuthenticated({ access_token: bOldLive, refresh_token: 'b-dead-refresh' });

    // Tab B's periodic poll starts a renewal before tab A's login lands; the
    // fetch is held open to model the real race (a round trip in flight).
    let resolveRefresh!: (r: Response) => void;
    const refreshGate = new Promise<Response>((res) => {
      resolveRefresh = res;
    });
    vi.stubGlobal('fetch', vi.fn(async () => refreshGate));
    const bRefresh = tabB.refreshAccessToken();

    // Tab A: a fresh tab/window where the operator just signed in.
    vi.resetModules();
    const tabA = await import('../src/stores/auth.svelte.ts');
    const aLive = jwtExpiringIn(900);
    tabA.setAuthenticated({ access_token: aLive, refresh_token: 'a-refresh' });

    // Tab B's in-flight renewal now resolves: the server rotates the bearer
    // for the session tab B presented (its own, still valid at this point).
    const bRenewed = jwtExpiringIn(900);
    resolveRefresh(jsonResponse(200, { access_token: bRenewed, refresh_token: 'b-refresh-2' }));
    await bRefresh;

    // Tab A's session must be exactly what tab A wrote — not wiped, and not
    // overwritten with tab B's rotated (but irrelevant) bearer.
    expect(window.localStorage.getItem('quicbaas_token')).toBe(aLive);
    expect(tabA.authStore.status).toBe('authenticated');
  });

  it('a sibling tab whose replay is still refused after renewal does not end a fresh login written in between', async () => {
    // Tab B: an already-open tab whose ENTIRE session the server has revoked
    // — every request 401s, renewal included in spirit, though here the
    // renewal itself still succeeds (the refresh token was still live) while
    // the resulting bearer is refused on arrival.
    vi.resetModules();
    const tabB = await import('../src/stores/auth.svelte.ts');
    const authFetchB = await import('../src/stores/authFetch.ts');
    const bLive = jwtExpiringIn(600);
    tabB.setAuthenticated({ access_token: bLive, refresh_token: 'b-refresh' });

    let resolveRefresh!: (r: Response) => void;
    const refreshGate = new Promise<Response>((res) => {
      resolveRefresh = res;
    });
    const base = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/v1/iam/refresh')) return refreshGate;
      // Every non-refresh call (the original request and its replay) 401s.
      return jsonResponse(401, { error: 'Invalid or expired token' });
    });
    vi.stubGlobal('fetch', base);
    authFetchB.installAuthFetch();

    // Tab B's original request 401s, which the interceptor picks up.
    const replay = window.fetch('https://baas.test/api/v1/orchestrator/jobs', {
      headers: { Authorization: `Bearer ${bLive}` },
    });

    // Tab A signs in fresh while tab B's renewal round trip is in flight.
    vi.resetModules();
    const tabA = await import('../src/stores/auth.svelte.ts');
    const aLive = jwtExpiringIn(900);
    tabA.setAuthenticated({ access_token: aLive, refresh_token: 'a-refresh' });

    // Tab B's renewal resolves with a rotated bearer for ITS OWN session.
    const bRenewed = jwtExpiringIn(900);
    resolveRefresh(jsonResponse(200, { access_token: bRenewed, refresh_token: 'b-refresh-2' }));
    await replay;
    authFetchB.uninstallAuthFetch();

    // Tab B's replay, using its own rotated bearer, still 401s (a fully
    // revoked session) and the interceptor's terminal state fires — but it
    // must act on TAB B's session, not on tab A's, which landed in between.
    expect(window.localStorage.getItem('quicbaas_token')).toBe(aLive);
    expect(tabA.authStore.status).toBe('authenticated');
  });
});

describe('every orchestrator + scheduler request carries the live bearer', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('a dashboard-shaped burst of parallel calls all carry the token set at login', async () => {
    const { createClient } = await import('@quicbaas/sdk');
    const { setAuthenticated, getValidAccessToken, setAuthStatus, clearSessionEndReason } =
      await import('../src/stores/auth.svelte.ts');
    const { installAuthFetch, uninstallAuthFetch } = await import('../src/stores/authFetch.ts');
    setAuthStatus('pending');
    clearSessionEndReason();

    const live = jwtExpiringIn(900);
    setAuthenticated({ access_token: live, refresh_token: 'refresh-1' });

    const calls: { url: string; auth: string }[] = [];
    const base = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const auth = new Headers(init?.headers ?? {}).get('Authorization') ?? '';
      calls.push({ url, auth });
      return jsonResponse(200, { jobs: [], sessions: [], agents: [], items: [], tasks: [] });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    try {
      const client = await createClient({
        transport: 'rest',
        baseURL: 'https://baas.test',
        token: async () => getValidAccessToken(),
      });

      // The System Dashboard's and the Attention Inbox's shape: several
      // widgets firing their own SDK calls concurrently on mount.
      await Promise.all([
        client.scheduler.listJobs({ limit: 1 }),
        client.orchestrator.listFleetNodes({ tenant_id: 'system' }),
        client.orchestrator.listSessions({ tenantID: 'system' }),
        client.orchestrator.listAttention('system'),
        client.orchestrator.listJobs('system'),
        client.orchestrator.listTasks('system'),
      ]);

      expect(calls).toHaveLength(6);
      for (const c of calls) {
        expect(c.auth, `missing bearer for ${c.url}`).toBe(`Bearer ${live}`);
      }
    } finally {
      uninstallAuthFetch();
    }
  });

  it('the same burst carries the RENEWED bearer when the login token is already inside the refresh skew', async () => {
    const { createClient } = await import('@quicbaas/sdk');
    const { setAuthenticated, getValidAccessToken, setAuthStatus, clearSessionEndReason } =
      await import('../src/stores/auth.svelte.ts');
    const { installAuthFetch, uninstallAuthFetch } = await import('../src/stores/authFetch.ts');
    setAuthStatus('pending');
    clearSessionEndReason();

    // Within the 60s skew window: getValidAccessToken's pre-emptive refresh
    // fires on the very first read, before any request goes out.
    const live = jwtExpiringIn(10);
    const renewed = jwtExpiringIn(900);
    setAuthenticated({ access_token: live, refresh_token: 'refresh-1' });

    let refreshCalls = 0;
    const calls: { url: string; auth: string }[] = [];
    const base = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/api/v1/iam/refresh')) {
        refreshCalls++;
        return jsonResponse(200, { access_token: renewed, refresh_token: 'refresh-2' });
      }
      const auth = new Headers(init?.headers ?? {}).get('Authorization') ?? '';
      calls.push({ url, auth });
      return jsonResponse(200, { jobs: [], sessions: [], agents: [], items: [], tasks: [] });
    });
    vi.stubGlobal('fetch', base);
    installAuthFetch();

    try {
      const client = await createClient({
        transport: 'rest',
        baseURL: 'https://baas.test',
        token: async () => getValidAccessToken(),
      });

      await Promise.all([
        client.scheduler.listJobs({ limit: 1 }),
        client.orchestrator.listFleetNodes({ tenant_id: 'system' }),
        client.orchestrator.listSessions({ tenantID: 'system' }),
        client.orchestrator.listAttention('system'),
        client.orchestrator.listJobs('system'),
        client.orchestrator.listTasks('system'),
      ]);

      // Single-flight: one renewal, shared by every concurrent caller.
      expect(refreshCalls).toBe(1);
      expect(calls).toHaveLength(6);
      for (const c of calls) {
        expect(c.auth, `missing/stale bearer for ${c.url}: got "${c.auth}"`).toBe(`Bearer ${renewed}`);
      }
    } finally {
      uninstallAuthFetch();
    }
  });
});

/**
 * Regression coverage for the SECOND half of the same investigation: the
 * fix above (requireCurrentCredential / endSessionIfCurrent's fingerprint
 * check) stops a sibling tab's LATE-ARRIVING renewal response from
 * corrupting shared storage, but it does nothing to stop TWO tabs from
 * concurrently presenting the SAME still-valid, single-use refresh token to
 * the server in the first place. Two windows on the same dashboard poll the
 * SAME access token's exp claim, so they cross the refresh-skew boundary
 * within moments of each other — not a rare interleaving, the ordinary shape
 * of an operator running the dashboard in two windows. The server treats a
 * second redemption of an already-rotated refresh token as reuse and revokes
 * the WHOLE family, including the access token the FIRST tab's redemption
 * just received — which is exactly what "Your session was renewed but the
 * server rejected the new credentials" was: a real renewal, undone a moment
 * later by a sibling's reuse of the token it was rotated from.
 *
 * The fix is a cross-tab mutex (the Web Locks API) around the redemption
 * round trip, with a re-check of the live refresh token after the lock is
 * acquired so the loser of the race adopts the winner's rotation instead of
 * repeating it. See withRefreshLock and performRenewal in
 * src/stores/auth.svelte.ts.
 */
describe('cross-tab refresh-token redemption is mutually exclusive', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    // @ts-expect-error test-only teardown of the mock installed above.
    delete globalThis.navigator.locks;
  });

  it('two tabs racing to renew the same session: the server sees only ONE redemption of the refresh token', async () => {
    const requestLock = installMockLocks();

    // Tab A: signs the session in.
    const tabA = await import('../src/stores/auth.svelte.ts');
    const live = jwtExpiringIn(10); // inside the 60s refresh-skew window
    tabA.setAuthenticated({ access_token: live, refresh_token: 'shared-refresh' });

    // Tab B: a second window/tab open on the SAME session, inheriting it from
    // shared storage rather than performing its own login.
    vi.resetModules();
    const tabB = await import('../src/stores/auth.svelte.ts');

    const redemptions: string[] = [];
    const renewed = jwtExpiringIn(900);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (!url.includes('/api/v1/iam/refresh')) return jsonResponse(404, {});
        const body = JSON.parse(String(init?.body ?? '{}')) as { refresh_token?: string };
        redemptions.push(body.refresh_token ?? '');
        return jsonResponse(200, { access_token: renewed, refresh_token: 'shared-refresh-2' });
      }),
    );

    // Both tabs decide, within moments of each other, that the session needs
    // renewing — the real-world trigger is each tab's own dashboard poll
    // crossing the same skew boundary independently.
    await Promise.all([tabA.refreshAccessToken(), tabB.refreshAccessToken()]);

    // The critical property: the refresh token was redeemed exactly once.
    // A second redemption of 'shared-refresh' is what the server's reuse
    // detection punishes by revoking the whole family — the ONE thing this
    // fix exists to prevent.
    expect(redemptions).toEqual(['shared-refresh']);
    expect(requestLock).toHaveBeenCalled();

    // The session is left in a single, live, authenticated state — not wiped,
    // not half-committed — carrying the ONE rotation the server actually
    // performed.
    expect(window.localStorage.getItem('quicbaas_token')).toBe(renewed);
    expect(window.localStorage.getItem('quicbaas_refresh_token')).toBe('shared-refresh-2');
    expect(tabA.authStore.status).toBe('authenticated');
  });

  it('the loser of the race adopts the winner\'s rotated bearer instead of ending the session', async () => {
    installMockLocks();

    const tabA = await import('../src/stores/auth.svelte.ts');
    const live = jwtExpiringIn(10);
    tabA.setAuthenticated({ access_token: live, refresh_token: 'shared-refresh' });

    vi.resetModules();
    const tabB = await import('../src/stores/auth.svelte.ts');
    // Tab B inherited this session from storage rather than logging in — in
    // the real app that is exactly what validateSession() on mount already
    // confirmed before either tab's poller ever triggers a renewal.
    tabB.setAuthStatus('authenticated');

    const renewed = jwtExpiringIn(900);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (!url.includes('/api/v1/iam/refresh')) return jsonResponse(404, {});
        return jsonResponse(200, { access_token: renewed, refresh_token: 'shared-refresh-2' });
      }),
    );

    const [tokenA] = await Promise.all([tabA.refreshAccessToken(), tabB.refreshAccessToken()]);

    // The winner (whichever tab's redemption the lock let through first)
    // gets the rotated bearer back from its own call.
    expect(tokenA).toBe(renewed);
    // Tab B's session was never ended by a "renewed but rejected" verdict —
    // the credential it lost the race for is exactly the one now live.
    expect(tabB.authStore.status).toBe('authenticated');
    expect(tabB.readAuthToken()).toBe(renewed);
  });
});

/**
 * Regression coverage for the third piece of the same investigation: an
 * uninitialized generationCredential/generationSID (both default to '', and
 * before this fix were seeded ONLY by a write this tab itself performs) made
 * a tab's very FIRST reconciliation of an INHERITED session — one it did not
 * itself log in to, e.g. any page reload — find a "mismatch" against the
 * real, completely unchanged token already in storage. That single spurious
 * mismatch both bumped the generation and fired announceSessionScopeChange(),
 * discarding every tenant-scoped cache for no reason on every reload, and put
 * every in-flight request whose caller had already captured the pre-bump
 * generation (authFetch.ts's `sessionGeneration()` snapshot) one bump away
 * from what endSessionIfCurrent expects to see.
 */
describe('an inherited session (page reload) is not mistaken for a new one', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('a fresh module load seeds generationCredential from the token already in storage', async () => {
    // Simulate a page reload: write a valid session directly to localStorage
    // (as a PRIOR page load would have left it) rather than logging in via
    // THIS module instance.
    const live = jwtExpiringIn(900);
    window.localStorage.setItem('quicbaas_token', live);
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-1');

    const scopeChanges = vi.fn();
    const auth = await import('../src/stores/auth.svelte.ts');
    auth.onSessionScopeChange(scopeChanges);

    const beforeKey = auth.authStore.sessionKey;
    // validateSession is the real entry point MainApp calls on every mount;
    // it always calls the reconciliation this seeding protects.
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(200, { valid: true })));
    await auth.validateSession();

    // No spurious "different session" verdict: the session key — which
    // embeds the generation — is unchanged, and no cache was told to release
    // data it still legitimately owns.
    expect(auth.authStore.sessionKey).toBe(beforeKey);
    expect(scopeChanges).not.toHaveBeenCalled();
    expect(auth.authStore.status).toBe('authenticated');
  });
});

// go-quicbaas#369: the SDK token factory (getValidAccessToken) attached
// whatever credential storage held without first asking whether it still
// belonged to this tab's session. A store snapshots its scope, then awaits its
// SDK call; if a sibling tab wrote a fresh login in between, the request went
// out as the sibling's session and the response committed under this tab's.
describe('the token factory reconciles the session before attaching a bearer', () => {
  const PROTECTED_ROW = { job_id: 'secret-A', tenant_id: 'burnin', state: 'executing' };

  // A token for one login: the sid is what tells a renewal of that login apart
  // from a fresh one by the same principal into the same tenant.
  function loginToken(sid: string, issue: number): string {
    const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
    const payload = btoa(
      JSON.stringify({
        uid: 'usr_A',
        tenant_id: 'burnin',
        sid,
        // Distinct bytes per issue, so a renewal is a different credential.
        iat: issue,
        exp: Math.floor(Date.now() / 1000) + 900,
      }),
    );
    return `${header}.${payload}.sig`;
  }

  // Records the bearer each data request carried. Only the FIRST data request
  // is answered with the protected row: a scope change makes the stores fetch
  // their data again under the new session, and those answers are empty so
  // they cannot be mistaken for the response under test. /validate answers
  // valid.
  function stubFetch(bearers: string[]): void {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        if (String(input).includes('/validate')) return jsonResponse(200, { valid: true });
        bearers.push(new Headers(init?.headers ?? {}).get('Authorization') ?? '');
        return jsonResponse(200, { jobs: bearers.length === 1 ? [PROTECTED_ROW] : [] });
      }),
    );
  }

  // Tab A, holding login s1 and validated, as a tab that inherited its session
  // is. Inheriting rather than signing in keeps the sign-in's own scope
  // announcement (and the fetches it starts) out of the window under test.
  // The sibling tab is modelled by writing straight to the shared
  // localStorage, with no storage event: the event is what used to close the
  // window, so withholding it is what exposes it.
  async function signedInTab(bearers: string[] = []) {
    window.localStorage.setItem('quicbaas_token', loginToken('s1', 1));
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-1');
    const auth = await import('../src/stores/auth.svelte.ts');
    const orchestrator = await import('../src/stores/orchestrator.svelte.ts');
    stubFetch(bearers);
    await auth.validateSession();
    expect(auth.authStore.status).toBe('authenticated');
    return { auth, orchestrator };
  }

  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("a sibling's fresh login moves the scope, so a response fetched under it does not commit", async () => {
    const bearers: string[] = [];
    const { auth, orchestrator } = await signedInTab(bearers);
    const before = auth.sessionGeneration();
    const keyBefore = auth.authStore.sessionKey;

    // The store pins its scope synchronously, then awaits the SDK; the sibling's
    // write lands in that await, before the token factory reads storage.
    const sibling = loginToken('s2', 2);
    const pending = orchestrator.refresh('jobs');
    window.localStorage.setItem('quicbaas_token', sibling);
    await pending;

    // The request went out as the sibling's session, so it must not have
    // committed as this tab's old one.
    expect(bearers[0]).toBe(`Bearer ${sibling}`);
    expect(auth.sessionGeneration()).toBeGreaterThan(before);
    expect(auth.authStore.sessionKey).not.toBe(keyBefore);
    expect(orchestrator.orchestratorStore.jobs).toHaveLength(0);
  });

  it("a sibling's renewal of the same login is used without ending the session", async () => {
    const bearers: string[] = [];
    const { auth, orchestrator } = await signedInTab(bearers);
    const before = auth.sessionGeneration();

    const renewed = loginToken('s1', 2);
    const pending = orchestrator.refresh('jobs');
    window.localStorage.setItem('quicbaas_token', renewed);
    await pending;

    expect(bearers).toEqual([`Bearer ${renewed}`]);
    expect(await auth.getValidAccessToken()).toBe(renewed);
    expect(auth.sessionGeneration()).toBe(before);
    expect(auth.authStore.status).toBe('authenticated');
    expect(orchestrator.orchestratorStore.jobs).toHaveLength(1);
  });

  it("a sibling's sign-out is adopted: no bearer is attached and the generation moves", async () => {
    const { auth } = await signedInTab();
    const before = auth.sessionGeneration();

    window.localStorage.removeItem('quicbaas_token');
    window.localStorage.removeItem('quicbaas_refresh_token');

    expect(await auth.getValidAccessToken()).toBe('');
    expect(auth.sessionGeneration()).toBeGreaterThan(before);
  });

  it("an inherited session's first token read does not start a new generation", async () => {
    window.localStorage.setItem('quicbaas_token', loginToken('s1', 1));
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-1');
    const auth = await import('../src/stores/auth.svelte.ts');
    const scopeChanges = vi.fn();
    auth.onSessionScopeChange(scopeChanges);
    const before = auth.sessionGeneration();

    expect(await auth.getValidAccessToken()).toBe(loginToken('s1', 1));
    expect(auth.sessionGeneration()).toBe(before);
    expect(scopeChanges).not.toHaveBeenCalled();
  });

  it("validateSession after a sibling's renewal still commits authenticated", async () => {
    const { auth } = await signedInTab();
    const before = auth.sessionGeneration();

    window.localStorage.setItem('quicbaas_token', loginToken('s1', 2));
    await auth.validateSession();

    expect(auth.authStore.status).toBe('authenticated');
    expect(auth.sessionGeneration()).toBe(before);
  });
});
