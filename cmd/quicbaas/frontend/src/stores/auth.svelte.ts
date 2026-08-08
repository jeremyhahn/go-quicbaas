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
 * Authentication state store — the single source of truth for whether the
 * current tab is signed in.
 *
 * Why this store exists
 * ---------------------
 * MainApp owns a route guard that bounces protected routes to /login whenever
 * authStatus is 'unauthenticated'. Before this store, authStatus lived inside
 * MainApp and was only ever re-evaluated on initial mount or on a cross-tab
 * `storage` event. A successful in-tab login (passkey, PIV, or self-register)
 * persisted the token and navigated to the dashboard, but authStatus stayed
 * 'unauthenticated' in THIS tab — so the guard fired on the very next tick and
 * silently bounced the freshly-authenticated user back to the login chooser.
 *
 * Hoisting authStatus into a shared reactive store fixes that: the login
 * completion paths call setAuthenticated() the moment the server returns
 * tokens, flipping the status in-tab so the guard sees 'authenticated' and
 * leaves the user on the dashboard.
 *
 * Token storage keys are owned here so every consumer reads/writes the same
 * localStorage slots ("quicbaas_token" / "quicbaas_refresh_token").
 *
 * Reactivity shape: module-scope `let x = $state(...)` + getter object — the
 * same pattern every other store in this app uses (connectionStore,
 * routerStore, sessionStore). A directly-exported $state proxy does not
 * propagate template updates across module boundaries in this Svelte 5 build.
 */

import { getSDKClient } from './sdkClient.ts';

export type AuthStatus = 'pending' | 'authenticated' | 'unauthenticated';

export const TOKEN_KEY = 'quicbaas_token';
export const REFRESH_TOKEN_KEY = 'quicbaas_refresh_token';

let status = $state<AuthStatus>('pending');

/**
 * Monotonic session generation. Every deliberate session transition (login,
 * logout) bumps it, so an ASYNC session check can tell whether the session it
 * was validating is still the current one.
 *
 * Without this a validateSession() that was in flight when the user signed out
 * resolves afterwards and writes 'authenticated' back — resurrecting a session
 * the user just ended. The window is small but real, and it is a security
 * regression, not a cosmetic one.
 */
let generation = 0;

export const authStore = {
  get status(): AuthStatus {
    return status;
  },
};

/** Read the persisted access token, or '' when none is stored. */
export function readAuthToken(): string {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(TOKEN_KEY) ?? '';
}

/** Tokens returned by a successful login/registration finish. */
export interface AuthTokens {
  access_token: string;
  refresh_token?: string;
}

/**
 * Persist tokens and flip the in-tab status to 'authenticated'. Called by
 * every login-completion path (passkey, PIV, self-register) the instant the
 * server confirms the session, so the MainApp route guard no longer bounces
 * the freshly-signed-in user back to /login.
 */
export function setAuthenticated(tokens: AuthTokens): void {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(TOKEN_KEY, tokens.access_token);
    if (tokens.refresh_token) {
      window.localStorage.setItem(REFRESH_TOKEN_KEY, tokens.refresh_token);
    }
  }
  generation++;
  status = 'authenticated';
}

/** Clear persisted tokens and flip the in-tab status to 'unauthenticated'. */
export function setUnauthenticated(): void {
  if (typeof window !== 'undefined') {
    window.localStorage.removeItem(TOKEN_KEY);
    window.localStorage.removeItem(REFRESH_TOKEN_KEY);
  }
  generation++;
  status = 'unauthenticated';
}

/** Force the status to a known value without touching token storage. */
export function setAuthStatus(next: AuthStatus): void {
  status = next;
}

/** Read the persisted refresh token, or '' when none is stored. */
export function readRefreshToken(): string {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(REFRESH_TOKEN_KEY) ?? '';
}

/**
 * Seconds until the given JWT expires, or null when the token is not a decodable
 * JWT with a numeric exp claim. Pure string/Base64 work — no signature
 * verification (the server is the authority); this only decides when to
 * pre-emptively refresh. Returning null (rather than 0) lets the caller leave an
 * undecodable/opaque token untouched instead of refreshing it needlessly.
 */
function secondsUntilExpiry(jwt: string): number | null {
  const parts = jwt.split('.');
  const claimsSegment = parts.length === 3 ? parts[1] : undefined;
  if (claimsSegment === undefined) return null;
  try {
    const json = atob(claimsSegment.replace(/-/g, '+').replace(/_/g, '/'));
    const claims = JSON.parse(json) as { exp?: number };
    if (typeof claims.exp !== 'number') return null;
    return claims.exp - Math.floor(Date.now() / 1000);
  } catch {
    return null;
  }
}

// Skew window: refresh when fewer than this many seconds remain so an in-flight
// request never races the access-token expiry.
const REFRESH_SKEW_SECONDS = 60;

// Single-flight guard so concurrent requests trigger at most one refresh call.
let _refreshInFlight: Promise<string> | null = null;

/**
 * Exchange the stored refresh token for a fresh access token via the public
 * /api/v1/auth/refresh endpoint, persist it, and return it. Returns '' (and
 * fails the session closed) when no refresh token is stored or the server
 * rejects it. Concurrent callers share one in-flight request.
 */
export async function refreshAccessToken(): Promise<string> {
  if (_refreshInFlight !== null) return _refreshInFlight;
  if (typeof window === 'undefined') return '';

  const refreshToken = readRefreshToken();
  if (!refreshToken) {
    setUnauthenticated();
    return '';
  }

  _refreshInFlight = (async (): Promise<string> => {
    try {
      const resp = await fetch(`${window.location.origin}/api/v1/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      if (!resp.ok) {
        // A rejected refresh token means the session is over — fail closed so
        // the MainApp route guard bounces the user to the login chooser.
        setUnauthenticated();
        return '';
      }
      const body = (await resp.json()) as {
        access_token?: string;
        refresh_token?: string;
      };
      const next = body.access_token ?? '';
      if (!next) {
        setUnauthenticated();
        return '';
      }
      // The server ROTATES the refresh credential: the one we just presented is
      // revoked. Persisting the replacement is what makes the SECOND renewal
      // work — storing only the access token would strand the session at the
      // next expiry with a dead refresh token.
      window.localStorage.setItem(TOKEN_KEY, next);
      if (body.refresh_token) {
        window.localStorage.setItem(REFRESH_TOKEN_KEY, body.refresh_token);
      }
      status = 'authenticated';
      return next;
    } catch {
      // Network failure: do NOT clear the session (transient); the caller falls
      // back to the existing token and the next request retries.
      return readAuthToken();
    } finally {
      _refreshInFlight = null;
    }
  })();

  return _refreshInFlight;
}

/**
 * Resolve the access token to attach to an outgoing request, refreshing it
 * pre-emptively when it is missing/expired or within the skew window. This is
 * the SDK token factory's source of truth (see sdkClient.ts): a long-lived tab
 * whose 15-minute access token expires mid-session transparently obtains a new
 * one from the refresh token instead of silently 401-ing.
 */
export async function getValidAccessToken(): Promise<string> {
  const token = readAuthToken();
  if (!token) return '';
  const remaining = secondsUntilExpiry(token);
  // Undecodable/opaque token, or comfortably valid: use it as-is.
  if (remaining === null || remaining > REFRESH_SKEW_SECONDS) return token;
  // Known to be expired or about to expire, and only worth a round-trip if we
  // actually hold a refresh token. refreshAccessToken returns the new token on
  // success, the still-valid existing token on a transient network failure, or
  // '' on a definitive rejection (after flipping the session to unauthenticated)
  // — return its result verbatim so a dead token is never attached.
  if (!readRefreshToken()) return token;
  return refreshAccessToken();
}

/**
 * Validate the persisted token against the server and set the status
 * accordingly. With no token we are unauthenticated; a server-rejected or
 * unreachable token also resolves to 'unauthenticated' so protected UI is
 * never shown without a confirmed session.
 */
export async function validateSession(): Promise<void> {
  // Snapshot the session this check belongs to; a login or logout that happens
  // while we await invalidates our verdict.
  const checking = generation;
  const commit = (next: AuthStatus): void => {
    if (generation === checking) status = next;
  };

  // Resolve through the refresh-aware accessor: on a page load more than 15
  // minutes after login the stored access token is expired, and validating it
  // verbatim would report 'unauthenticated' for a session that is in fact
  // still renewable.
  const token = await getValidAccessToken();
  if (!token) {
    commit('unauthenticated');
    return;
  }
  try {
    const client = await getSDKClient();
    const ok = await client.auth.authValidate(token);
    commit(ok ? 'authenticated' : 'unauthenticated');
  } catch {
    commit('unauthenticated');
  }
}
