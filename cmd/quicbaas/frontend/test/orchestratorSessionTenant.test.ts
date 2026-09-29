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
 * Session-tenant scoping tests (go-quicbaas#309).
 *
 * The bug: the SPA pinned its tenant to the literal 'system', so an operator
 * authenticated into any other tenant issued `?tenant_id=system` on every
 * orchestrator query, was refused, and saw empty Agent Fleet, Sessions, Tasks,
 * Workflows and Jobs views.
 *
 * These assert the REQUEST URLs, not the store value. A store that holds the
 * right string while the wire carries the wrong one is the failure mode being
 * guarded against, so the check has to be made where the tenant actually
 * travels: the SDK builds these URLs, and a fetch spy records them.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import {
  TOKEN_KEY,
  authStore,
  refreshAccessToken,
  setAuthenticated,
  setUnauthenticated,
  validateSession,
} from '../src/stores/auth.svelte.ts';
import { resetSDKClient } from '../src/stores/sdkClient.ts';
import { fleetStore, refreshFleet } from '../src/stores/fleetStore.svelte.ts';
import {
  sessionStore,
  openSession,
  refreshSessions,
} from '../src/stores/sessionStore.svelte.ts';
import {
  SYSTEM_TENANT_ID,
  loadJobs,
  orchestratorStore,
  refresh,
  resetOrchestratorStore,
  setActiveTenant,
} from '../src/stores/orchestrator.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build an unsigned JWT carrying the claims a live session bearer carries. */
/** Serial that makes every minted credential distinct. */
let credentialSerial = 0;

/** Serial that makes every minted LOGIN distinct. */
let loginSerial = 0;

/**
 * Build an unsigned JWT carrying the claims a live session bearer carries.
 *
 * Every call returns a DISTINCT credential even for identical claims — a second
 * login by the same principal into the same tenant is exactly that, and a
 * fixture that returned the same string could not express it (go-quicbaas#326).
 *
 * `sid` is the server's per-login identifier. It defaults to a fresh one, so a
 * bare call means "another login"; a caller modelling a RENEWAL passes the sid
 * of the login being renewed, which is what the server does — it mints sid once
 * per login and replays it across that login's rotations. Passing '' models a
 * bearer minted before the claim existed.
 */
function sessionToken(
  tenantID: string,
  roles: string[] = ['operator'],
  uid = 'usr_01HZ',
  sid: string = `sess-${(loginSerial += 1)}`,
): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({
      sub: 'operator@example.test',
      uid,
      tenant_id: tenantID,
      roles,
      exp: Math.floor(Date.now() / 1000) + 900,
      jti: `cred-${(credentialSerial += 1)}`,
      ...(sid === '' ? {} : { sid }),
    }),
  );
  return `${header}.${payload}.sig`;
}

/**
 * Build a bearer whose `sid` claim is whatever JSON value is handed in,
 * including one the server would never send.
 *
 * The SPA's decoder parses and asserts the claim shape rather than checking it,
 * so "what if this is not a string" is a real input, not a hypothetical.
 */
function tokenWithRawSid(rawSid: unknown): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({
      sub: 'operator@example.test',
      uid: 'usr_U',
      tenant_id: 'burnin',
      roles: ['operator'],
      exp: Math.floor(Date.now() / 1000) + 900,
      jti: `cred-${(credentialSerial += 1)}`,
      sid: rawSid,
    }),
  );
  return `${header}.${payload}.sig`;
}

/**
 * Complete a login for `tenantID` through the real login-completion path, and
 * return the session identifier that login carries so a case can go on to model
 * a renewal OF it rather than another login.
 */
function signIn(tenantID: string): string {
  const sid = `sess-${(loginSerial += 1)}`;
  setAuthenticated({
    access_token: sessionToken(tenantID, ['operator'], 'usr_01HZ', sid),
    refresh_token: 'refresh-1',
  });
  return sid;
}

const originalFetch = window.fetch;
let requested: string[] = [];

/**
 * Rows the stub answers with, keyed by the tenant in the request's query string,
 * so a test can tell WHOSE data ended up in the store. Anything unlisted answers
 * empty.
 */
let rowsByTenant: Record<string, unknown[]> = {};

/**
 * Gates that hold a given tenant's responses until a test releases them, so one
 * tenant's answer can still be in flight while another's completes.
 */
let holdByTenant: Record<string, Promise<void>> = {};

/** Access token the stubbed renewal endpoint hands back, when a test cares. */
let renewalToken: string | null = null;

/** Record every request URL and answer with that tenant's configured rows. */
function stubFetch(): void {
  const spy = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    requested.push(url);
    const tenant = new URL(url, window.location.origin).searchParams.get('tenant_id') ?? '';
    if (url.includes('/api/v1/iam/refresh')) {
      // The real renewal: a new access token for the SAME session.
      return new Response(
        JSON.stringify({
          access_token: renewalToken ?? sessionToken('burnin'),
          refresh_token: 'refresh-rotated',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    }
    const rows = rowsByTenant[tenant] ?? [];
    const gate = holdByTenant[tenant];
    if (gate !== undefined) await gate;
    return new Response(
      JSON.stringify({ jobs: rows, nodes: rows, sessions: rows, tasks: rows, valid: true }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    );
  });
  Object.defineProperty(window, 'fetch', { writable: true, configurable: true, value: spy });
}

/**
 * One row belonging to `tenant`, carrying every id field the store's dedupe
 * uses, so the same fixture serves jobs, agents, sessions and tasks.
 */
function row(tenant: string): Record<string, unknown> {
  return {
    job_id: `row-${tenant}`,
    id: `row-${tenant}`,
    session_id: `row-${tenant}`,
    tenant_id: tenant,
    state: 'executing',
  };
}

/** The row ids currently visible across all four tenant-scoped collections. */
function visibleIDs(): Record<string, string[]> {
  return {
    jobs: orchestratorStore.jobs.map((j) => j.job_id),
    agents: orchestratorStore.fleetNodes.map((a) => a.id),
    sessions: orchestratorStore.sessions.map((x) => x.session_id),
    tasks: orchestratorStore.tasks.map((t) => t.id),
  };
}

/**
 * Let the scope-change listeners' refetches run to completion.
 *
 * Signing in and switching tenant both announce a scope change, and the stores
 * answer it with fetches nobody awaits. A case that wants to attribute requests
 * to one of those events has to let the previous one finish first.
 */
async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
}

/** Query-string tenants of every orchestrator request made so far. */
function requestedTenants(from = 0): string[] {
  return requested
    .slice(from)
    .filter((u) => u.includes('/api/v1/orchestrator/'))
    .map((u) => new URL(u, window.location.origin).searchParams.get('tenant_id'))
    .filter((t): t is string => t !== null);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('orchestrator queries are scoped to the session tenant', () => {
  beforeEach(() => {
    requested = [];
    rowsByTenant = {};
    holdByTenant = {};
    renewalToken = null;
    // Order matters: clear the session first (which re-derives the claims cache
    // from an empty store), then reset the per-resource fetch timestamps so
    // stale-while-revalidate does not skip the network on the second case.
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    stubFetch();
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: originalFetch,
    });
    vi.restoreAllMocks();
  });

  it('sends the tenant_id from the session claim on every orchestrator query', async () => {
    signIn('burnin');

    await refresh();

    const tenants = requestedTenants();
    expect(tenants.length).toBeGreaterThanOrEqual(4);
    expect(new Set(tenants)).toEqual(new Set(['burnin']));
    // The literal that caused #309 must appear in no request at all.
    expect(tenants).not.toContain(SYSTEM_TENANT_ID);
  });

  it('reaches jobs, the fleet, sessions and tasks each scoped to that tenant', async () => {
    signIn('burnin');

    await refresh();

    for (const path of ['orchestrator/jobs', 'fleet', 'orchestrator/sessions', 'orchestrator/tasks']) {
      const resource = path;
      const hit = requested.find((u) => u.includes(`/api/v1/${path}?`));
      expect(hit, `no request for ${resource}`).toBeDefined();
      expect(new URL(hit as string, window.location.origin).searchParams.get('tenant_id')).toBe(
        'burnin',
      );
    }
  });

  it('exposes the session tenant as the active tenant without any stored preference', () => {
    signIn('burnin');
    expect(orchestratorStore.activeTenantID).toBe('burnin');

    // The claim is re-read on every load; nothing is persisted beyond the token
    // itself, so a session for a different tenant moves the whole UI with it.
    signIn('acme');
    expect(orchestratorStore.activeTenantID).toBe('acme');
  });

  it('falls back to the system tenant only when there is no session to read', () => {
    expect(orchestratorStore.activeTenantID).toBe(SYSTEM_TENANT_ID);
  });

  it('honours an explicit tenant switch for the session it was made in', async () => {
    signIn('system');
    // Signing in legitimately loads the session's own tenant, so only what
    // happens AFTER the switch is evidence about the switch — and its fetches
    // must have landed before the count is taken, or they arrive afterwards and
    // are misattributed to the switch.
    await settle();
    const afterLogin = requested.length;

    setActiveTenant('burnin');
    expect(orchestratorStore.activeTenantID).toBe('burnin');

    await refresh();
    expect(new Set(requestedTenants(afterLogin))).toEqual(new Set(['burnin']));
  });

  it('drops an explicit tenant switch when a different session takes over', () => {
    signIn('system');
    setActiveTenant('burnin');
    expect(orchestratorStore.activeTenantID).toBe('burnin');

    // A new principal signs in. The previous operator's selection must not
    // survive into their session and send queries at a tenant they cannot see.
    signIn('acme');
    expect(orchestratorStore.activeTenantID).toBe('acme');
  });

  it('treats selecting your own tenant as clearing the override', () => {
    signIn('system');
    setActiveTenant('burnin');
    setActiveTenant('system');
    expect(orchestratorStore.activeTenantID).toBe('system');
  });

  it('drops an override when a different principal signs into the same tenant', () => {
    // Two system admins share a tenant, so the tenant claim alone cannot tell
    // them apart. One selects 'burnin' and leaves; the next must not inherit it.
    setAuthenticated({ access_token: sessionToken('system', ['admin'], 'usr_A') });
    setActiveTenant('burnin');
    expect(orchestratorStore.activeTenantID).toBe('burnin');

    setAuthenticated({ access_token: sessionToken('system', ['admin'], 'usr_B') });
    expect(orchestratorStore.activeTenantID).toBe('system');
  });

  it('stops naming the old tenant when the session ends in another tab', async () => {
    signIn('burnin');
    expect(orchestratorStore.activeTenantID).toBe('burnin');

    // What a cross-tab sign-out looks like from here: the token is simply gone
    // and MainApp answers the `storage` event by re-validating.
    window.localStorage.removeItem(TOKEN_KEY);
    window.localStorage.removeItem('quicbaas_refresh_token');
    await validateSession();

    expect(orchestratorStore.activeTenantID).toBe(SYSTEM_TENANT_ID);
  });

  it("never shows a new session the previous session's cached rows", async () => {
    rowsByTenant = { burnin: [row('burnin')], acme: [] };
    signIn('burnin');
    await refresh();
    expect(orchestratorStore.jobs.map((j) => j.job_id)).toEqual(['row-burnin']);

    // Tenant A signs out, tenant B signs in. B must not see A's rows for even
    // one render, and the 15-second freshness window must not be able to
    // suppress the corrective fetch.
    setUnauthenticated();
    signIn('acme');
    expect(orchestratorStore.jobs).toEqual([]);
    expect(orchestratorStore.fleetNodes).toEqual([]);
    expect(orchestratorStore.sessions).toEqual([]);
    expect(orchestratorStore.tasks).toEqual([]);

    const before = requested.length;
    await loadJobs();
    expect(requested.length).toBeGreaterThan(before);
    expect(orchestratorStore.jobs).toEqual([]);
  });

  it("does not show one admin's rows to the next admin in the same tenant", async () => {
    rowsByTenant = { system: [row('system')] };
    setAuthenticated({ access_token: sessionToken('system', ['admin'], 'usr_A') });
    await refresh();
    expect(orchestratorStore.jobs.map((j) => j.job_id)).toEqual(['row-system']);

    // Same tenant, different person. The tenant claim alone cannot tell them
    // apart, so the cache has to be keyed on the principal too.
    setAuthenticated({ access_token: sessionToken('system', ['admin'], 'usr_B') });
    expect(orchestratorStore.jobs).toEqual([]);

    // And the freshness window from A's fetch must not suppress B's own.
    const before = requested.length;
    await loadJobs();
    expect(requested.length).toBeGreaterThan(before);
  });

  it('cannot let a response for a departed session overwrite the live one', async () => {
    rowsByTenant = { burnin: [row('burnin')], acme: [row('acme')] };
    signIn('burnin');

    // burnin's answer is held in flight.
    let releaseBurnin: () => void = () => {};
    holdByTenant['burnin'] = new Promise<void>((resolve) => {
      releaseBurnin = resolve;
    });
    const stale = refresh();

    // The session moves to acme and acme's own load COMPLETES, so the cache is
    // legitimately in scope and populated. This is what makes the assertion
    // below load-bearing: the read gate is satisfied, so only the commit guard
    // can stop the late answer from landing.
    signIn('acme');
    await refresh();
    expect(visibleIDs()).toEqual({
      jobs: ['row-acme'],
      agents: ['row-acme'],
      sessions: ['row-acme'],
      tasks: ['row-acme'],
    });

    releaseBurnin();
    await stale;

    expect(visibleIDs()).toEqual({
      jobs: ['row-acme'],
      agents: ['row-acme'],
      sessions: ['row-acme'],
      tasks: ['row-acme'],
    });
  });

  it('cannot let a response for a switched-away tenant overwrite the current one', async () => {
    rowsByTenant = { system: [row('system')], burnin: [row('burnin')] };
    signIn('system');

    let releaseSystem: () => void = () => {};
    holdByTenant['system'] = new Promise<void>((resolve) => {
      releaseSystem = resolve;
    });
    const stale = refresh();

    // Same session, but the header switcher moved to another tenant, and that
    // tenant's load completed.
    setActiveTenant('burnin');
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);

    releaseSystem();
    await stale;

    expect(visibleIDs()).toEqual({
      jobs: ['row-burnin'],
      agents: ['row-burnin'],
      sessions: ['row-burnin'],
      tasks: ['row-burnin'],
    });
  });

  it('gives two logins by the same person into the same tenant different session keys', () => {
    // Pins the generation in sessionKey directly. The behavioural tests below
    // cannot isolate it: the key and the on-transition reset are deliberately
    // redundant, so either one alone produces the same observable result. This
    // asserts the key itself.
    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });
    const first = authStore.sessionKey;

    setUnauthenticated();
    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });

    expect(authStore.sessionKey).not.toBe(first);
    // Same principal and tenant, so everything but the generation is identical.
    expect(authStore.sessionKey).toContain('usr_A');
    expect(authStore.sessionKey).toContain('burnin');
  });

  it('keys the session on the server identifier of the login, not only on a counter', () => {
    // The generation is this tab's own bookkeeping, and it is only right when
    // every transition was observed. The sid is the SERVER's answer to which
    // login this is, so putting it in the key makes two logins distinct by
    // construction rather than by this module having noticed (go-quicbaas#326).
    setAuthenticated({
      access_token: sessionToken('burnin', ['operator'], 'usr_A', 'sess-alpha'),
    });

    expect(authStore.sessionKey).toContain('sess-alpha');

    setUnauthenticated();
    setAuthenticated({
      access_token: sessionToken('burnin', ['operator'], 'usr_A', 'sess-beta'),
    });

    expect(authStore.sessionKey).toContain('sess-beta');
    expect(authStore.sessionKey).not.toContain('sess-alpha');
  });

  it('gives the same person signing back in a clean slate, not their last session', async () => {
    // The case a tenant/uid-only key cannot see: identical principal, identical
    // tenant, but a NEW login. Grants can have changed in between, so the second
    // session must re-authorize against the server rather than inheriting a
    // cache the first one filled.
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);

    setUnauthenticated();
    expect(orchestratorStore.jobs).toEqual([]);

    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });
    expect(visibleIDs()).toEqual({ jobs: [], agents: [], sessions: [], tasks: [] });

    // And the previous session's freshness must not suppress this one's fetch.
    const before = requested.length;
    await loadJobs();
    expect(requested.length).toBeGreaterThan(before);
  });

  it('releases the cache on sign-out rather than only hiding it', async () => {
    rowsByTenant = { burnin: [row('burnin')] };
    signIn('burnin');
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);

    // Signing out CLEARS protected data rather than merely making it
    // unreachable. Reaching past the getters to the loader proves it: with the
    // rows and their freshness timestamps genuinely gone, the next load has to
    // go to the network — a cache that had only been hidden would answer from
    // memory and issue nothing.
    setUnauthenticated();
    const before = requested.length;
    await loadJobs();
    expect(requested.length).toBeGreaterThan(before);
  });

  it('signs this tab out when the session ends in another tab', async () => {
    // The assertion this file was missing. The cross-tab cases below check the
    // TENANT and the cache; none of them checked whether the tab still claims
    // to be signed in — so a fail-open in which the protected shell keeps
    // rendering after a cross-tab sign-out went unnoticed.
    signIn('burnin');
    await validateSession();
    expect(authStore.status).toBe('authenticated');

    window.localStorage.removeItem(TOKEN_KEY);
    window.localStorage.removeItem('quicbaas_refresh_token');
    await validateSession();

    expect(authStore.status).toBe('unauthenticated');
  });

  it("commits the replacement session's verdict when another tab signs in", async () => {
    setUnauthenticated();
    expect(authStore.status).toBe('unauthenticated');

    // Another tab signs someone in; this tab learns of it through the same door
    // and must adopt the result rather than discarding its own verdict.
    window.localStorage.setItem(TOKEN_KEY, sessionToken('burnin', ['operator'], 'usr_A'));
    await validateSession();

    expect(authStore.status).toBe('authenticated');
    expect(orchestratorStore.activeTenantID).toBe('burnin');
  });

  it('treats a cross-tab re-login as a new session, not a resumed one', async () => {
    // The one door that does not go through setAuthenticated/setUnauthenticated:
    // another tab replaces the session and this tab learns of it via `storage`,
    // which MainApp answers by re-validating. Same person, same tenant, but a
    // different login — the cache must not come back with it.
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);

    // Another tab signs out: the token simply vanishes from storage.
    window.localStorage.removeItem(TOKEN_KEY);
    window.localStorage.removeItem('quicbaas_refresh_token');
    await validateSession();
    expect(orchestratorStore.jobs).toEqual([]);
    expect(authStore.status).toBe('unauthenticated');

    // Another tab signs the same person back in, same tenant.
    window.localStorage.setItem(TOKEN_KEY, sessionToken('burnin', ['operator'], 'usr_A'));
    await validateSession();

    expect(authStore.status).toBe('authenticated');
    expect(visibleIDs()).toEqual({ jobs: [], agents: [], sessions: [], tasks: [] });
    const before = requested.length;
    await loadJobs();
    expect(requested.length).toBeGreaterThan(before);
  });

  // A case that used to sit here asserted "a refresh is not a new session" while
  // SIMULATING the refresh with a direct localStorage write — which is what a
  // DIFFERENT tab does, not what this tab's renewal does. It passed only because
  // the code shared its mistake: reconciliation compared claims, and a
  // replacement carrying identical claims looked like no change at all. Under
  // #326 that write is correctly a new session, so the case is replaced by the
  // pair below, which drive the two events through the paths that really produce
  // them: a storage write for the replacement, refreshAccessToken() for the
  // renewal.

  it('drops the cache when another tab logs in as the same person, same tenant', async () => {
    // The door round 3 did not cover. Its test signed out and back in, and a
    // REMOVAL changes the identity twice — to empty, then to the new session —
    // so the generation moved and the case passed. A direct replacement changes
    // the claims ZERO times: same uid, same tenant, different credential.
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({
      access_token: sessionToken('burnin', ['operator'], 'usr_U'),
      refresh_token: 'refresh-1',
    });
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
    const keyBefore = authStore.sessionKey;

    // Another tab logs in. One storage write, no sign-out in between.
    window.localStorage.setItem(
      TOKEN_KEY,
      sessionToken('burnin', ['operator'], 'usr_U'),
    );
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-2');
    await validateSession();

    expect(authStore.sessionKey).not.toBe(keyBefore);
    expect(visibleIDs()).toEqual({ jobs: [], agents: [], sessions: [], tasks: [] });

    // And the previous login's freshness must not suppress this one's fetch.
    const before = requested.length;
    await loadJobs();
    expect(requested.length).toBeGreaterThan(before);
  });

  it('drops the cache when a renewal redeems a credential another tab installed', async () => {
    // The refresh credential lives in SHARED storage. If another tab replaces
    // the session and this tab renews before reconciling, it redeems their
    // refresh token and receives their session's bearer — which, persisted as
    // this generation's credential, would make the later reconciliation see a
    // match, skip the bump, and leave the previous login's caches in scope.
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({
      access_token: sessionToken('burnin', ['operator'], 'usr_U'),
      refresh_token: 'refresh-1',
    });
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
    const keyBefore = authStore.sessionKey;

    // Another tab logs in as the same principal into the same tenant.
    window.localStorage.setItem(
      TOKEN_KEY,
      sessionToken('burnin', ['operator'], 'usr_U'),
    );
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-from-other-tab');

    // This tab renews before it has reconciled — the 401 recovery path, or a
    // token near expiry.
    renewalToken = sessionToken('burnin', ['operator'], 'usr_U');
    await refreshAccessToken();

    expect(authStore.sessionKey).not.toBe(keyBefore);
    expect(visibleIDs()).toEqual({ jobs: [], agents: [], sessions: [], tasks: [] });
  });

  it('does not drop the cache when this tab renews its own credential', async () => {
    // The other half. A renewal replaces the credential within ONE session, and
    // this tab wrote it — so it must not look like a new login.
    rowsByTenant = { burnin: [row('burnin')] };
    const sid = signIn('burnin');
    await refresh();
    const keyBefore = authStore.sessionKey;

    // The real renewal path, not a test-only shortcut: refreshAccessToken posts
    // to the renewal endpoint and persists what comes back. The renewed bearer
    // carries the SAME sid, because the server replays a login's identifier
    // across its rotations.
    renewalToken = sessionToken('burnin', ['operator'], 'usr_01HZ', sid);
    await refreshAccessToken();
    await validateSession();

    expect(authStore.sessionKey).toBe(keyBefore);
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
  });

  it('keeps the cache when another tab renews the session this tab is in', async () => {
    // What the credential alone cannot decide (go-quicbaas#326). A sibling tab
    // renewing THIS login writes a credential this tab did not author, which is
    // byte-wise indistinguishable from somebody logging in — so before the sid
    // claim this tab had to assume the worst and drop everything, roughly every
    // fourteen minutes per sibling tab. The server's per-login identifier is
    // what makes the renewal recognisable as the same session.
    rowsByTenant = { burnin: [row('burnin')] };
    const sid = signIn('burnin');
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
    const keyBefore = authStore.sessionKey;

    // The sibling's rotation lands in shared storage: new credential, same login.
    window.localStorage.setItem(
      TOKEN_KEY,
      sessionToken('burnin', ['operator'], 'usr_01HZ', sid),
    );
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-rotated-elsewhere');
    await validateSession();

    expect(authStore.sessionKey).toBe(keyBefore);
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
  });

  it('treats a credential carrying no session identifier as another session', async () => {
    // Fail-closed. A bearer minted before the claim existed decodes to no sid,
    // and so does the one this generation was established with — but two
    // absences are not a match. Comparing them as equal would make EVERY
    // unidentified credential look like a renewal of every other, which is the
    // exact leak this claim was added to close.
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({
      access_token: sessionToken('burnin', ['operator'], 'usr_U', ''),
      refresh_token: 'refresh-1',
    });
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
    const keyBefore = authStore.sessionKey;

    window.localStorage.setItem(
      TOKEN_KEY,
      sessionToken('burnin', ['operator'], 'usr_U', ''),
    );
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-2');
    await validateSession();

    expect(authStore.sessionKey).not.toBe(keyBefore);
    expect(visibleIDs()).toEqual({ jobs: [], agents: [], sessions: [], tasks: [] });
  });

  it('treats a session identifier that is not a string as no identifier at all', async () => {
    // The decoder asserts the claim shape, it does not check it, so `sid` can
    // arrive as any JSON value. A number or a boolean is not an identifier, and
    // two credentials carrying the same non-identifier must not be read as one
    // session — which is what a bare `!== ''` guard would do, because 0 and
    // false are both unequal to ''.
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({ access_token: tokenWithRawSid(0), refresh_token: 'refresh-1' });
    await refresh();
    expect(visibleIDs().jobs).toEqual(['row-burnin']);
    const keyBefore = authStore.sessionKey;

    window.localStorage.setItem(TOKEN_KEY, tokenWithRawSid(0));
    window.localStorage.setItem('quicbaas_refresh_token', 'refresh-2');
    await validateSession();

    expect(authStore.sessionKey).not.toBe(keyBefore);
    expect(visibleIDs()).toEqual({ jobs: [], agents: [], sessions: [], tasks: [] });
  });

  it('resolves the tenant from the stored bearer on a cold load, with nothing in memory', async () => {
    // A reload is the case the fix has to survive: no store state, no login
    // event, just the persisted token. Re-importing the modules from scratch is
    // what a fresh page gives them.
    window.localStorage.setItem(TOKEN_KEY, sessionToken('burnin'));
    vi.resetModules();

    const store = await import('../src/stores/orchestrator.svelte.ts');
    expect(store.orchestratorStore.activeTenantID).toBe('burnin');
  });
});

// ---------------------------------------------------------------------------
// The sibling stores keep their own SDK-typed caches of the same data and read
// their tenant from orchestratorStore. They are what FleetList, SessionList,
// FleetNodeDetailView and SystemDashboard actually render, so a scope gate that
// stopped at orchestratorStore would leave the leak plainly visible in the very
// views #309 is about.
// ---------------------------------------------------------------------------

describe('the sibling agent and session caches are scoped to the same session', () => {
  beforeEach(() => {
    requested = [];
    rowsByTenant = {};
    holdByTenant = {};
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    stubFetch();
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: originalFetch,
    });
    vi.restoreAllMocks();
  });

  it("never shows a new session the previous session's agents or sessions", async () => {
    rowsByTenant = { burnin: [row('burnin')], acme: [] };
    signIn('burnin');
    await Promise.all([refreshFleet(), refreshSessions()]);
    expect(fleetStore.nodes.map((a) => a.id)).toEqual(['row-burnin']);
    expect(sessionStore.sessions.map((x) => x.session_id)).toEqual(['row-burnin']);

    signIn('acme');
    expect(fleetStore.nodes).toEqual([]);
    expect(fleetStore.onlineNodes).toEqual([]);
    expect(sessionStore.sessions).toEqual([]);
  });

  it('hides an open session and its transcript once the session that opened it is gone', async () => {
    rowsByTenant = { burnin: [row('burnin')] };
    signIn('burnin');
    await refreshSessions();

    const opened = sessionStore.sessions[0];
    expect(opened).toBeDefined();
    // openSession logs `subscription error ... AbortSignal` here. That is jsdom
    // refusing a cross-realm AbortSignal, not a failure of anything under test:
    // this case is about the session STATE being hidden, and no live stream is
    // needed to show that. The teardown of an actual stream needs a controlled
    // subscription double and lives in sessionSubscriptionTeardown.test.ts.
    await openSession(opened as NonNullable<typeof opened>);
    expect(sessionStore.activeSession?.session_id).toBe('row-burnin');

    signIn('acme');
    // A conversation transcript is the last thing that should survive into
    // someone else's session.
    expect(sessionStore.activeSession).toBeNull();
    expect(sessionStore.events).toEqual([]);
  });

  it('refills the sibling caches when the tenant switches, with no component mounted', async () => {
    // Isolated from MainApp on purpose. Inside the mounted app this also happens
    // by accident: refreshFleet reads activeTenantID synchronously, so the
    // auxiliary $effect that calls it re-runs on a tenant change. That accident
    // is not the mechanism being relied on — it would evaporate the moment that
    // read moved back after an await — so the guarantee is pinned here, where
    // only the scope-change bus can produce it.
    rowsByTenant = { system: [row('system')], burnin: [row('burnin')] };
    setAuthenticated({ access_token: sessionToken('system', ['admin'], 'usr_A') });
    await Promise.all([refreshFleet(), refreshSessions()]);
    await settle();
    expect(fleetStore.nodes.map((a) => a.id)).toEqual(['row-system']);

    setActiveTenant('burnin');
    await settle();

    expect(fleetStore.nodes.map((a) => a.id)).toEqual(['row-burnin']);
    expect(sessionStore.sessions.map((x) => x.session_id)).toEqual(['row-burnin']);
  });

  it('gives the same person signing back in an empty fleet and session list', async () => {
    rowsByTenant = { burnin: [row('burnin')] };
    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });
    await Promise.all([refreshFleet(), refreshSessions()]);
    expect(fleetStore.nodes).toHaveLength(1);
    expect(sessionStore.sessions).toHaveLength(1);

    setUnauthenticated();
    setAuthenticated({ access_token: sessionToken('burnin', ['operator'], 'usr_A') });

    expect(fleetStore.nodes).toEqual([]);
    expect(fleetStore.onlineNodes).toEqual([]);
    expect(sessionStore.sessions).toEqual([]);
  });

  // The live stream's TEARDOWN is verified in sessionSubscriptionTeardown.test.ts,
  // not here. A test that lived at this spot claimed to check it and did not:
  // this suite drives the real SDK over a stubbed fetch, and under jsdom the SSE
  // path dies on a cross-realm AbortSignal before a subscription is ever
  // established — so there was no stream to tear down and the test passed with
  // closeSubscription() deleted. It needs a controlled subscription double,
  // which is why it has its own file.

  it('cannot let a departed session\'s agent listing overwrite the live one', async () => {
    rowsByTenant = { burnin: [row('burnin')], acme: [row('acme')] };
    signIn('burnin');

    let releaseBurnin: () => void = () => {};
    holdByTenant['burnin'] = new Promise<void>((resolve) => {
      releaseBurnin = resolve;
    });
    const stale = refreshFleet();

    signIn('acme');
    await refreshFleet();
    expect(fleetStore.nodes.map((a) => a.id)).toEqual(['row-acme']);

    releaseBurnin();
    await stale;

    expect(fleetStore.nodes.map((a) => a.id)).toEqual(['row-acme']);
  });

  it('cannot let a departed session\'s session listing overwrite the live one', async () => {
    rowsByTenant = { burnin: [row('burnin')], acme: [row('acme')] };
    signIn('burnin');

    let releaseBurnin: () => void = () => {};
    holdByTenant['burnin'] = new Promise<void>((resolve) => {
      releaseBurnin = resolve;
    });
    const stale = refreshSessions();

    signIn('acme');
    await refreshSessions();
    expect(sessionStore.sessions.map((x) => x.session_id)).toEqual(['row-acme']);

    releaseBurnin();
    await stale;

    expect(sessionStore.sessions.map((x) => x.session_id)).toEqual(['row-acme']);
  });
});
