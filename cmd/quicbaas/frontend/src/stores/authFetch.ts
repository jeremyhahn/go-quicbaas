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
 * Authenticated-fetch recovery layer.
 *
 * Why this exists
 * ───────────────
 * Access tokens live 15 minutes by design. The SDK token factory
 * (getValidAccessToken) refreshes PRE-EMPTIVELY, which handles the ordinary
 * case, but it cannot cover every one:
 *
 *   • a request that was already in flight when the token lapsed,
 *   • clock skew between browser and server,
 *   • a token the server revoked early (admin action, key rotation).
 *
 * In all of those the server answers 401 and, before this layer existed, the
 * page simply died with `{"error":"Invalid or expired token"}`.
 *
 * This installs ONE interceptor over window.fetch that, on a 401 from a
 * same-origin API request, renews the session once and replays the request
 * with the fresh bearer. It is deliberately a single global choke point rather
 * than per-call retry logic sprinkled through the SDK and the stores: there is
 * exactly one place to reason about, and it covers every client class plus any
 * direct fetch a view makes.
 *
 * Guarantees:
 *   • single-flight — concurrent 401s share one renewal (refreshAccessToken
 *     owns that lock), so a page that fires ten parallel requests performs one
 *     refresh, not ten.
 *   • replay at most once, and a replay that 401s ENDS the session. A bearer
 *     minted seconds ago and refused by the very next call is not a stale
 *     token, so renewing again cannot help: clear the credentials once and
 *     record why. That terminal state is what bounds the loop, and the bound
 *     is structural rather than a retry counter — once the credentials are
 *     gone isRenewable() is false for every subsequent request, so no further
 *     renewal is even attempted. The end is scoped to the session it was
 *     decided for (endSessionIfCurrent), because two awaits separate the
 *     verdict from acting on it. Before any of this, refresh→200→validate→401
 *     repeated at request rate with no backoff and no end (go-quicbaas#256:
 *     >1000 requests in about a minute, every view empty, the operator bounced
 *     to login with a stale banner).
 *   • the refresh call itself is never intercepted (no recursion). The path is
 *     imported from the auth store rather than restated here: when the renewal
 *     endpoint moved (go-quicbaas#221) a second copy of the literal would have
 *     silently stopped matching, and the interceptor would recurse on the very
 *     request that ends the recursion.
 *   • unauthenticated requests (no Authorization header and no stored session)
 *     are passed straight through: a 401 from a public endpoint is a real
 *     answer, not a stale-token symptom.
 */

import {
  REFRESH_PATH,
  endSessionIfCurrent,
  refreshAccessToken,
  readAuthToken,
  readRefreshToken,
  sessionGeneration,
} from './auth.svelte.ts';

/** Prefix identifying API requests that carry a session bearer. */
const API_PREFIX = '/api/';

let _installed = false;
let _originalFetch: typeof fetch | null = null;

/** Resolve a fetch input to a URL string without throwing on odd inputs. */
function requestURL(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  return input.url;
}

/** Read the Authorization header from either the init or a Request input. */
function authorizationOf(input: RequestInfo | URL, init?: RequestInit): string {
  const fromInit = new Headers(init?.headers ?? {}).get('Authorization');
  if (fromInit) return fromInit;
  if (typeof input !== 'string' && !(input instanceof URL)) {
    return input.headers.get('Authorization') ?? '';
  }
  return '';
}

/**
 * Decide whether a 401 for this request is worth retrying after a renewal.
 * Only same-origin API calls made on behalf of a signed-in user qualify.
 */
function isRenewable(input: RequestInfo | URL, init?: RequestInit): boolean {
  const url = requestURL(input);
  if (url.includes(REFRESH_PATH)) return false;

  let path: string;
  try {
    path = new URL(url, window.location.origin).pathname;
  } catch {
    return false;
  }
  if (!path.startsWith(API_PREFIX)) return false;

  // Something must identify the caller as a signed-in user: either the request
  // already carried a bearer, or we hold one to attach on replay.
  if (!authorizationOf(input, init) && !readAuthToken()) return false;

  // Without a refresh credential there is nothing to renew with.
  return readRefreshToken() !== '';
}

/** Replay a request with a freshly minted bearer. */
function withBearer(init: RequestInit | undefined, token: string): RequestInit {
  const headers = new Headers(init?.headers ?? {});
  headers.set('Authorization', `Bearer ${token}`);
  return { ...init, headers };
}

/**
 * installAuthFetch replaces window.fetch with the recovery wrapper. Calling it
 * more than once is a no-op, so app bootstrap and tests can both invoke it.
 */
export function installAuthFetch(): void {
  if (_installed || typeof window === 'undefined') return;
  _originalFetch = window.fetch.bind(window);
  const original = _originalFetch;

  window.fetch = async (
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const resp = await original(input, init);
    if (resp.status !== 401 || !isRenewable(input, init)) return resp;

    // Snapshot BEFORE the renewal: everything below is a verdict about the
    // session as it stands now, and two awaits separate it from acting on that
    // verdict.
    const snapshot = sessionGeneration();

    // Single-flight: concurrent 401s all await the same renewal.
    const token = await refreshAccessToken();
    if (!token) return resp;

    // A Request input carries its own (now stale) Authorization header, so
    // rebuild it from the URL to guarantee the replay uses the new bearer.
    const replay = await original(requestURL(input), withBearer(init, token));
    if (replay.status !== 401) return replay;

    // Terminal state. The server minted this bearer moments ago and refused
    // it on the very next call, so the credential is not what is wrong and a
    // further renewal would only repeat the exchange. End the session with the
    // reason attached.
    //
    // Scoped to the session this chain started in. A burst of concurrent 401s
    // shares one renewal and arrives here together, so the first to land ends
    // the session and the generation bump makes the rest no-ops; and a chain
    // whose replay resolves only after the user has signed in again cannot
    // clear the new session out from under them.
    endSessionIfCurrent(
      snapshot,
      'Your session was renewed but the server rejected the new credentials. ' +
        'Please sign in again.',
    );
    return replay;
  };

  _installed = true;
}

/**
 * uninstallAuthFetch restores the original window.fetch. Intended for tests so
 * one case's interceptor cannot leak into the next.
 */
export function uninstallAuthFetch(): void {
  if (!_installed || _originalFetch === null) return;
  window.fetch = _originalFetch;
  _originalFetch = null;
  _installed = false;
}
