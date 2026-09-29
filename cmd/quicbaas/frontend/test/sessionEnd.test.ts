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
 * Ending a session is one thing, however it is reached (go-quicbaas#309).
 *
 * Three defects on this branch were each a call site that flipped the auth
 * status and forgot part of what ending a session means — an explicit sign-out,
 * a session removed in another tab, and a validation the server rejected. The
 * third left the rejected token in storage and the scoped getters still serving
 * the previous principal's rows.
 *
 * So this file does not test those three cases. It states the PROPERTY and runs
 * every known path through it, and it fails if a fourth path is added that does
 * not go through the one owner.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import {
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  authStore,
  refreshAccessToken,
  setAuthenticated,
  setAuthStatus,
  setUnauthenticated,
  validateSession,
} from '../src/stores/auth.svelte.ts';
import { resetSDKClient } from '../src/stores/sdkClient.ts';
import {
  SYSTEM_TENANT_ID,
  orchestratorStore,
  refresh,
  resetOrchestratorStore,
} from '../src/stores/orchestrator.svelte.ts';
import { fleetStore, refreshFleet } from '../src/stores/fleetStore.svelte.ts';
import { sessionStore, refreshSessions } from '../src/stores/sessionStore.svelte.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function sessionToken(uid = 'usr_A', tenant = 'burnin'): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({
      uid,
      tenant_id: tenant,
      roles: ['operator'],
      exp: Math.floor(Date.now() / 1000) + 900,
    }),
  );
  return `${header}.${payload}.sig`;
}

const PROTECTED_ROW = {
  job_id: 'secret-A',
  id: 'secret-A',
  session_id: 'secret-A',
  tenant_id: 'burnin',
  state: 'executing',
};

/** Answers every orchestrator call with one row, and authValidate as directed. */
function stubFetch(validAnswer: boolean | 'throw'): void {
  Object.defineProperty(window, 'fetch', {
    writable: true,
    configurable: true,
    value: vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
      const url = String(input);
      if (url.includes('/validate')) {
        if (validAnswer === 'throw') throw new TypeError('network down');
        return new Response(JSON.stringify({ valid: validAnswer }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(
        JSON.stringify({
          jobs: [PROTECTED_ROW],
          nodes: [PROTECTED_ROW],
          sessions: [PROTECTED_ROW],
          tasks: [PROTECTED_ROW],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    }),
  });
}

/** Sign in and load protected data into every scoped cache. */
async function signInAndLoad(): Promise<void> {
  setAuthenticated({ access_token: sessionToken(), refresh_token: 'refresh-1' });
  await refresh();
  await Promise.all([refreshFleet(), refreshSessions()]);
  expect(orchestratorStore.jobs).toHaveLength(1);
  expect(fleetStore.nodes).toHaveLength(1);
  expect(sessionStore.sessions).toHaveLength(1);
}

// ---------------------------------------------------------------------------
// Every way a session can end. A new one goes in this table.
// ---------------------------------------------------------------------------

interface EndingPath {
  name: string;
  /** How the server answers authValidate while this path runs. */
  validAnswer: boolean | 'throw';
  end: () => Promise<void> | void;
}

const ENDING_PATHS: EndingPath[] = [
  {
    name: 'the operator signs out',
    validAnswer: true,
    end: () => setUnauthenticated(),
  },
  {
    name: 'a renewal is refused and the session is failed closed',
    validAnswer: true,
    end: () => setUnauthenticated('Your session could not be renewed.'),
  },
  {
    name: 'the session is removed in another tab',
    validAnswer: true,
    end: async () => {
      window.localStorage.removeItem(TOKEN_KEY);
      window.localStorage.removeItem(REFRESH_TOKEN_KEY);
      await validateSession();
    },
  },
  {
    name: 'the server rejects the session on validation',
    validAnswer: false,
    end: () => validateSession(),
  },
  {
    name: 'the server cannot be reached to confirm the session',
    validAnswer: 'throw',
    end: () => validateSession(),
  },
  {
    name: 'the status is forced to unauthenticated directly',
    validAnswer: true,
    end: () => setAuthStatus('unauthenticated'),
  },
];

// ---------------------------------------------------------------------------
// The property
// ---------------------------------------------------------------------------

describe('however a session ends, it ends completely', () => {
  beforeEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    vi.restoreAllMocks();
  });

  for (const path of ENDING_PATHS) {
    it(`clears everything when ${path.name}`, async () => {
      stubFetch(path.validAnswer);
      await signInAndLoad();
      const keyBefore = authStore.sessionKey;

      await path.end();

      // 1. The tab knows the session is over.
      expect(authStore.status).toBe('unauthenticated');
      // 2. The credentials are gone, so nothing can present them again.
      expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
      expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
      // 3. The scope key moved, so nothing cached under the old one matches.
      expect(authStore.sessionKey).not.toBe(keyBefore);
      // 4. The tenant is no longer named.
      expect(authStore.tenantID).toBe('');
      expect(orchestratorStore.activeTenantID).toBe(SYSTEM_TENANT_ID);
      // 5. No scoped store will hand out the previous principal's data.
      expect(orchestratorStore.jobs).toEqual([]);
      expect(orchestratorStore.fleetNodes).toEqual([]);
      expect(orchestratorStore.sessions).toEqual([]);
      expect(orchestratorStore.tasks).toEqual([]);
      expect(fleetStore.nodes).toEqual([]);
      expect(fleetStore.onlineNodes).toEqual([]);
      expect(sessionStore.sessions).toEqual([]);
      expect(sessionStore.activeSession).toBeNull();
      expect(sessionStore.events).toEqual([]);
    });
  }
});

// ---------------------------------------------------------------------------
// The structural guard
//
// The property above can only cover paths someone remembered to add to the
// table. This is what catches the one they did not: it reads the module and
// fails if a second way to write the auth status appears at all. A new ending
// path cannot then be written as a bare status change — it has to go through
// applyStatus, which routes to endSession, which is what the property asserts.
// ---------------------------------------------------------------------------

describe('the auth status has exactly one writer', () => {
  it('assigns `status` in one place, so no path can end a session silently', () => {
    const source = readFileSync(
      resolve(__dirname, '../src/stores/auth.svelte.ts'),
      'utf8',
    );

    // Assignments only: the declaration is at column 0 (`let status = $state`),
    // every assignment is indented inside a function.
    //
    // A guard, not a proof: it is textual, so an assignment split across lines
    // would slip past it. That is worth having anyway — it catches the thing
    // people actually write, and the failure message says what to do instead.
    const assignments = source
      .split('\n')
      .map((line, i) => ({ line, n: i + 1 }))
      .filter(({ line }) => /^\s+status = /.test(line));

    expect(
      assignments.map((a) => `${a.n}: ${a.line.trim()}`),
      'Every auth-status transition must go through applyStatus(), which routes ' +
        "'unauthenticated' to endSession(). A second assignment here means a path " +
        'can mark the tab signed out while leaving the credentials in storage and ' +
        'the scoped caches readable — the exact defect this guard exists to stop. ' +
        'Route the new path through applyStatus and add it to ENDING_PATHS above.',
    ).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// A verdict belongs to the session it was made for.
//
// These are about the single predicate guarding validateSession's commit. The
// three narrower ones tried before it each passed the cases below except one,
// and the one each missed is a different case — which is why the predicate had
// to become the session identity rather than a proxy for it.
// ---------------------------------------------------------------------------

describe("a validation verdict cannot be applied to a different session", () => {
  beforeEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    vi.restoreAllMocks();
  });

  it("does not authenticate session B on session A's answer", async () => {
    const tokenA = sessionToken('usr_A', 'burnin');
    const tokenB = sessionToken('usr_B', 'acme');

    // The server would affirm A and refuse B. If A's verdict were applied to B,
    // the shell would go protected on a credential the server rejects.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const url = String(input);
        if (url.includes('/validate')) {
          const body = String(init?.body ?? '');
          const affirm = body.includes(tokenA);
          // Another tab replaces A with B while this validation is in flight.
          window.localStorage.setItem(TOKEN_KEY, tokenB);
          window.localStorage.setItem(REFRESH_TOKEN_KEY, 'refresh-B');
          return new Response(JSON.stringify({ valid: affirm }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          });
        }
        return new Response(JSON.stringify({ jobs: [], nodes: [], sessions: [], tasks: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: tokenA, refresh_token: 'refresh-A' });
    const keyA = authStore.sessionKey;
    await validateSession();

    // The defect was being authenticated AS A while carrying B's bearer: A's
    // identity on screen, A's caches in scope, B's credential on the wire. The
    // fix adopts B at commit, so the tab is consistently B — which is a correct
    // state, since B is a session another tab just established and the server
    // authorises every request against the bearer anyway.
    //
    // Asserting merely "not authenticated" would be weaker and would pass for
    // the wrong reason: what matters is that no part of the tab is still A.
    expect(authStore.sessionKey).not.toBe(keyA);
    expect(authStore.tenantID).toBe('acme');
    expect(orchestratorStore.jobs).toEqual([]);
  });

  it('does not resurrect a session that ended during the validation round trip', async () => {
    // The window the predicate exists for. Everything before authValidate is
    // settled by ordering — the credential and the identity are read together,
    // after every earlier await. What remains is the round trip itself, and a
    // session can end inside it: the authFetch interceptor gives up on a
    // credential, or the operator signs out. An affirmative answer must not then
    // put the shell back into its protected state.
    const token = sessionToken('usr_A', 'burnin');

    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
        if (String(input).includes('/validate')) {
          // The session ends WHILE the server is being asked.
          setUnauthenticated('Signed out elsewhere.');
          return new Response(JSON.stringify({ valid: true }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          });
        }
        return new Response(JSON.stringify({ jobs: [], nodes: [], sessions: [], tasks: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: token, refresh_token: 'refresh-1' });
    await validateSession();

    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
  });

  it('validates the session it adopts, and a rejected bearer ends it', async () => {
    // Adoption and validation are two steps, and this pins both.
    //
    // The call that ADOPTS refuses the stale verdict and moves the tab onto B —
    // but does not validate B, so it briefly shows the status it already had.
    // The same cross-tab write that installed B fires a storage event, which
    // MainApp answers with another validateSession; that is the call that
    // validates B. A bearer the server rejects therefore ends the session.
    //
    // Without the second step this would be a tab trusting a credential it never
    // checked, so the assertion below is the one that matters.
    const tokenA = sessionToken('usr_A', 'burnin');
    const tokenB = sessionToken('usr_B', 'acme');

    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        if (String(input).includes('/iam/validate')) {
          // The bearer travels in the Authorization header, not the body.
          const auth = new Headers(init?.headers ?? {}).get('Authorization') ?? '';
          const askedAboutA = auth.includes(tokenA);
          if (askedAboutA) {
            // Another tab installs B while the server is being asked about A.
            window.localStorage.setItem(TOKEN_KEY, tokenB);
            window.localStorage.setItem(REFRESH_TOKEN_KEY, 'refresh-B');
          }
          // The server affirms A and REJECTS B.
          return new Response(JSON.stringify({ valid: askedAboutA }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          });
        }
        return new Response(JSON.stringify({ jobs: [], nodes: [], sessions: [], tasks: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: tokenA, refresh_token: 'refresh-A' });
    await validateSession();

    // Adopted: the tab is B, not A. A's verdict was not applied to it.
    expect(authStore.tenantID).toBe('acme');

    // The storage event that delivered B drives the validation of B.
    await validateSession();

    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
  });

  it('keeps the verdict when a refresh rotates the credential within one session', async () => {
    // The other side of the same predicate: a rotated token is the SAME session,
    // so its verdict must still count. A bytes-equality check failed here and
    // stranded the tab on 'pending'.
    const first = sessionToken('usr_A', 'burnin');
    window.localStorage.setItem(TOKEN_KEY, first);
    window.localStorage.setItem(REFRESH_TOKEN_KEY, 'refresh-1');
    setAuthStatus('pending');

    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        // Same principal, same tenant, new credential.
        window.localStorage.setItem(TOKEN_KEY, sessionToken('usr_A', 'burnin'));
        return new Response(JSON.stringify({ valid: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    await validateSession();

    expect(authStore.status).toBe('authenticated');
  });
});

// ---------------------------------------------------------------------------
// A renewal commits for the session that started it, or commits nothing.
//
// `refreshAccessToken` decides before an await and writes after it, so the
// session can change underneath. These force that window open with a gated
// response — a fetch double that resolves synchronously leaves no window and
// the assertions would hold vacuously.
// ---------------------------------------------------------------------------

describe('a renewal cannot commit for a session that is no longer there', () => {
  let release: () => void;
  let gate: Promise<void>;
  let renewed: string;

  beforeEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    renewed = sessionToken('usr_LATE', 'burnin');
    gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        // Held open until the test has moved the session on.
        await gate;
        return new Response(
          JSON.stringify({ access_token: renewed, refresh_token: 'refresh-LATE' }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }),
    });
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    vi.restoreAllMocks();
  });

  it('commits nothing when the operator signed out while it was in flight', async () => {
    setAuthenticated({ access_token: sessionToken(), refresh_token: 'refresh-1' });
    const inFlight = refreshAccessToken();

    setUnauthenticated();
    expect(authStore.status).toBe('unauthenticated');

    release();
    await inFlight;

    // Not "the access token was skipped" — nothing at all.
    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
  });

  it('commits nothing when a different session signed in while it was in flight', async () => {
    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    // The operator signs out and back in as someone else.
    setUnauthenticated();
    const tokenB = sessionToken('usr_B', 'acme');
    setAuthenticated({ access_token: tokenB, refresh_token: 'refresh-B' });
    const keyB = authStore.sessionKey;

    release();
    await inFlight;

    // B is untouched: not signed out by A's verdict, and not overwritten by A's
    // rotated credentials.
    expect(authStore.status).toBe('authenticated');
    expect(authStore.sessionKey).toBe(keyB);
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(tokenB);
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-B');
  });

  it('does not sign out the session that replaced the one it was renewing', async () => {
    // The failure paths need the same scoping as the success path, and for the
    // same reason: a refusal is a verdict about the credential that was
    // presented, not about whoever is signed in when it arrives.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        return new Response(JSON.stringify({ error_description: 'revoked' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    setUnauthenticated();
    const tokenB = sessionToken('usr_B', 'acme');
    setAuthenticated({ access_token: tokenB, refresh_token: 'refresh-B' });

    release();
    await inFlight;

    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(tokenB);
  });

  it('still ends the session when the refusal is about the session in effect', async () => {
    // The other half: a refusal that IS about the current session must end it,
    // or a fix that simply never signs anyone out would pass the case above.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        return new Response(JSON.stringify({ error_description: 'revoked' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    release();
    await inFlight;

    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(authStore.endReason).toContain('could not be renewed');
  });

  it('does not hand a new session\'s bearer back to the old session on a network failure', async () => {
    // The transient path answers with whatever is in storage, on the reasoning
    // that the credential is probably still live. After a transition it is
    // live — for somebody else. The SDK token factory would attach it to a
    // request the OLD session started.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        throw new TypeError('network down');
      }),
    });

    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    setUnauthenticated();
    const tokenB = sessionToken('usr_B', 'acme');
    setAuthenticated({ access_token: tokenB, refresh_token: 'refresh-B' });

    release();
    const handedBack = await inFlight;

    expect(handedBack).not.toBe(tokenB);
    expect(handedBack).toBe('');
  });

  it('does not share one session\'s renewal with a caller in another', async () => {
    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const first = refreshAccessToken();

    // A transition, then a second caller renewing the NEW session. It must run
    // its own renewal rather than awaiting the first one's answer, which is
    // about a session it is not in.
    setUnauthenticated();
    setAuthenticated({ access_token: sessionToken('usr_B', 'acme'), refresh_token: 'refresh-B' });
    const second = refreshAccessToken();

    // Promise IDENTITY proves nothing here — refreshAccessToken is an async
    // function, so every call returns a fresh promise whatever it does
    // internally. The observable is whether a second round trip happened.
    expect(window.fetch).toHaveBeenCalledTimes(2);

    release();
    await Promise.allSettled([first, second]);
  });

  it('still shares one renewal between concurrent callers in the SAME session', async () => {
    // The property the single-flight exists for, which the scoping must not
    // cost: one round trip, however many callers.
    setAuthenticated({ access_token: sessionToken(), refresh_token: 'refresh-1' });
    const a = refreshAccessToken();
    const b = refreshAccessToken();

    expect(window.fetch).toHaveBeenCalledTimes(1);

    release();
    expect(await a).toBe(renewed);
    expect(await b).toBe(renewed);
    expect(window.fetch).toHaveBeenCalledTimes(1);
  });

  it('does not tear down a newer session when a 2xx carries no access token', async () => {
    // 1c, first terminal path. A late FAILURE is the same defect with the sign
    // flipped: it must not end whichever session happens to exist when it lands.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        return new Response(JSON.stringify({ refresh_token: 'rotated' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    setUnauthenticated();
    const tokenB = sessionToken('usr_B', 'acme');
    setAuthenticated({ access_token: tokenB, refresh_token: 'refresh-B' });

    release();
    await inFlight;

    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(tokenB);
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-B');
  });

  it('does not tear down a newer session when the response body will not parse', async () => {
    // 1c, second terminal path.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        return new Response('this is not json', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    setUnauthenticated();
    const tokenB = sessionToken('usr_B', 'acme');
    setAuthenticated({ access_token: tokenB, refresh_token: 'refresh-B' });

    release();
    await inFlight;

    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(tokenB);
  });

  it('still ends the session when a 2xx carries no access token for the CURRENT session', async () => {
    // The other half, so a fix that simply never tears down would not pass.
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        return new Response(JSON.stringify({ refresh_token: 'rotated' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });

    setAuthenticated({ access_token: sessionToken(), refresh_token: 'refresh-1' });
    const inFlight = refreshAccessToken();

    release();
    await inFlight;

    expect(authStore.status).toBe('unauthenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(authStore.endReason).toContain('no access token');
  });

  it("a finishing renewal does not deregister a different session's", async () => {
    // Two renewals alive at once is deliberate — a departed session's renewal
    // must not be shared with the session that replaced it. What must NOT
    // follow is the first one to finish clearing the slot the second is
    // registered in: the next caller would then find it empty and start a
    // SECOND redemption of the same rotating refresh token, which the server
    // treats as reuse and answers by revoking the family. That is a live
    // session dying because the operator clicked around.
    const gates: Record<string, () => void> = {};
    const tokenForB = sessionToken('usr_B', 'acme');
    let renewals = 0;
    const fetchSpy = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      // Only renewals are gated and counted. The scope-change listeners fire
      // their own orchestrator requests on every sign-in, and counting those
      // would drown the signal.
      if (!String(input).includes('/api/v1/iam/refresh')) {
        return new Response(JSON.stringify({ jobs: [], nodes: [], sessions: [], tasks: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      renewals++;
      const presented = String(JSON.parse(String(init?.body ?? '{}')).refresh_token);
      await new Promise<void>((resolve) => {
        gates[presented] = resolve;
      });
      // Each session's renewal answers with a credential shaped like ITS
      // principal, so a caller receiving the wrong session's token is visible
      // rather than hidden behind an identical fixture.
      return new Response(
        JSON.stringify({
          access_token: presented === 'refresh-B' ? tokenForB : sessionToken('usr_A'),
          refresh_token: `rotated-${presented}`,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    });
    const renewalCount = (): number => renewals;
    Object.defineProperty(window, 'fetch', {
      writable: true, configurable: true, value: fetchSpy,
    });

    setAuthenticated({ access_token: sessionToken('usr_A'), refresh_token: 'refresh-A' });
    const a = refreshAccessToken();

    setUnauthenticated();
    setAuthenticated({ access_token: sessionToken('usr_B', 'acme'), refresh_token: 'refresh-B' });
    const b = refreshAccessToken();

    await vi.waitFor(() => {
      expect(Object.keys(gates)).toHaveLength(2);
    });
    expect(renewalCount()).toBe(2);

    // A settles FIRST, while B's renewal is still outstanding.
    gates['refresh-A']?.();
    await a;

    // A third caller, still in B's session. It must join B's flight, not open a
    // second redemption of refresh-B.
    const c = refreshAccessToken();
    expect(renewalCount()).toBe(2);

    gates['refresh-B']?.();
    expect(await b).toBe(tokenForB);
    // c joined B's flight, so it receives B's credential — not A's, and not ''.
    expect(await c).toBe(tokenForB);
    expect(renewalCount()).toBe(2);

    // A's answer was about a session that no longer exists, so it is discarded
    // rather than handed back.
    expect(await a).toBe('');
  });

  it('commits normally when the session is still the one that asked', async () => {
    // The window is open in this case too — it is just not used. Without this,
    // a fix that refused every renewal would pass the two cases above.
    setAuthenticated({ access_token: sessionToken(), refresh_token: 'refresh-1' });
    const inFlight = refreshAccessToken();

    release();
    const result = await inFlight;

    expect(result).toBe(renewed);
    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe(renewed);
    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-LATE');
  });
});

// ---------------------------------------------------------------------------
// A session's refresh credential is the one that session was issued.
//
// Two paths issue a session — a login and a renewal — and they disagreed: the
// renewal cleared the slot when the server sent no replacement, the login left
// whatever was there. After a second login that is the PREVIOUS principal's
// credential, where the next renewal would redeem it (go-quicbaas#314).
//
// The cases vary the SHAPE of what the server sends, not just present-versus-
// absent: mutation-checking varies the implementation and would not have found
// the whitespace case, because every mutation would have used a well-formed one.
// ---------------------------------------------------------------------------

describe("a login does not inherit the previous principal's refresh credential", () => {
  beforeEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    vi.restoreAllMocks();
  });

  const issuedNone: Array<{ shape: string; tokens: Record<string, unknown> }> = [
    { shape: 'the field is absent', tokens: {} },
    { shape: 'the field is an empty string', tokens: { refresh_token: '' } },
    { shape: 'the field is whitespace', tokens: { refresh_token: '   ' } },
    { shape: 'the field is a newline', tokens: { refresh_token: '\n' } },
  ];

  for (const { shape, tokens } of issuedNone) {
    it(`clears it when ${shape}`, () => {
      setAuthenticated({
        access_token: sessionToken('usr_A', 'burnin'),
        refresh_token: 'refresh-A',
      });
      expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-A');

      setAuthenticated({
        access_token: sessionToken('usr_B', 'acme'),
        ...(tokens as { refresh_token?: string }),
      });

      // Principal B's session must not hold principal A's renewal credential.
      expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
    });
  }

  it('stores the credential when the login is issued one', () => {
    // The other half, so a fix that simply always cleared would not pass.
    setAuthenticated({
      access_token: sessionToken('usr_A', 'burnin'),
      refresh_token: 'refresh-A',
    });
    setAuthenticated({
      access_token: sessionToken('usr_B', 'acme'),
      refresh_token: 'refresh-B',
    });

    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-B');
  });

  it('trims a credential rather than storing it padded', () => {
    setAuthenticated({
      access_token: sessionToken('usr_A', 'burnin'),
      refresh_token: '  refresh-padded  ',
    });

    expect(window.localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('refresh-padded');
  });
});

// ---------------------------------------------------------------------------
// What a renewal hands back belongs to the session that asked for it.
//
// The boundary compares the CREDENTIAL against storage rather than the
// generation, because the generation is in-memory and moves only once
// reconciliation has noticed a cross-tab write — so in exactly the window that
// matters it still matches and would wave the replacement bearer through.
// ---------------------------------------------------------------------------

describe('a renewal does not hand back a credential another tab installed', () => {
  beforeEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    vi.restoreAllMocks();
  });

  it('returns nothing when storage was replaced during a transient failure', async () => {
    // The transient path answers with whatever is in storage, on the reasoning
    // that the credential is probably still live. After a replacement it IS
    // live — for someone else — and the SDK token factory would attach it to a
    // request this session started.
    const mine = sessionToken('usr_A', 'burnin');
    // SAME LENGTH as `mine`, different bytes: length alone cannot tell them
    // apart, so this exercises the hash rather than the cheap component.
    const theirs = `${mine.slice(0, -4)}ZZZZ`;
    expect(theirs).toHaveLength(mine.length);
    expect(theirs).not.toBe(mine);

    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        throw new TypeError('network down');
      }),
    });

    setAuthenticated({ access_token: mine, refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();

    // Another tab replaces the credential. Nothing in memory moves.
    window.localStorage.setItem(TOKEN_KEY, theirs);

    release();
    const handedBack = await inFlight;

    expect(handedBack).not.toBe(theirs);
    expect(handedBack).toBe('');
  });

  it('still hands back its own credential when storage was not replaced', async () => {
    // The other half, so a boundary that refused everything would not pass.
    const mine = sessionToken('usr_A', 'burnin');
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        await gate;
        throw new TypeError('network down');
      }),
    });

    setAuthenticated({ access_token: mine, refresh_token: 'refresh-A' });
    const inFlight = refreshAccessToken();
    release();

    expect(await inFlight).toBe(mine);
  });
});

// ---------------------------------------------------------------------------
// The fingerprint's WIDTH is chosen, not defaulted.
// ---------------------------------------------------------------------------

describe('the credential fingerprint separates tokens a 32-bit hash cannot', () => {
  // A real FNV-1a 32-bit collision, found by search: same length, identical
  // 32-bit hash, different bytes. Under a single-pass fingerprint these two
  // credentials look like the SAME session, so reconciliation would not treat
  // one replacing the other as a session change — the previous login's caches
  // would stay in scope. This is the input a well-formed-token mutation set
  // never supplies.
  const COLLIDING_A = 'RQ-DjSjzWORJYdnUvLUbz2KD';
  const COLLIDING_B = '2vExF-3POoPQ_fbOJ_VxOJhq';

  beforeEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: vi.fn(async (): Promise<Response> => {
        return new Response(JSON.stringify({ valid: true, jobs: [], nodes: [], sessions: [], tasks: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    });
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
    vi.restoreAllMocks();
  });

  it('confirms the premise: the pair really does collide under one 32-bit pass', () => {
    // Asserted rather than assumed, so the case cannot quietly stop testing
    // what it claims if someone edits the constants.
    const fnv32 = (token: string): number => {
      let h = 0x811c9dc5;
      for (let i = 0; i < token.length; i++) {
        h = Math.imul(h ^ token.charCodeAt(i), 0x01000193) >>> 0;
      }
      return h;
    };
    expect(COLLIDING_A).toHaveLength(COLLIDING_B.length);
    expect(COLLIDING_A).not.toBe(COLLIDING_B);
    expect(fnv32(COLLIDING_A)).toBe(fnv32(COLLIDING_B));
  });

  it('treats one replacing the other as a session change', async () => {
    // Drives the SHIPPED fingerprint through reconciliation rather than
    // recomputing it here: a local copy would agree with itself no matter what
    // the store does, which is no test at all.
    setAuthenticated({ access_token: COLLIDING_A, refresh_token: 'refresh-A' });
    const keyBefore = authStore.sessionKey;

    // Another tab installs the colliding credential.
    window.localStorage.setItem(TOKEN_KEY, COLLIDING_B);
    await validateSession();

    // Under a single 32-bit pass these fingerprints are equal, reconciliation
    // sees "same credential" and the key does not move.
    expect(authStore.sessionKey).not.toBe(keyBefore);
  });
});
