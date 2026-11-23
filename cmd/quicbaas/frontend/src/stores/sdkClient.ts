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
 * SDK client singleton store.
 *
 * Exposes a lazily-initialised Client instance. Transport is auto-detected:
 * Wails desktop → 'wails'; browser → 'rest' with baseURL from window.location.
 *
 * All components share this single instance so subscriptions and state
 * mutations don't conflict with each other.
 *
 * Authentication
 * ──────────────
 * The client is a singleton created once — typically BEFORE the user has
 * logged in (the login views fetch it to run the passkey/PIV handshake). We
 * therefore cannot bake a static bearer token into the config at construction
 * time. Instead we pass a token *factory* (`() => readAuthToken()`), which the
 * SDK invokes lazily on every request. After setAuthenticated() persists the
 * access token to localStorage, every subsequent authenticated call — orchestrator,
 * KMS, IAM, vectors, etc. — automatically attaches `Authorization: Bearer <token>`
 * with the current token. Public endpoints (login/begin, invite GET, public
 * health) simply run before a token exists; the factory returns '' and the SDK
 * omits the header, which those endpoints tolerate.
 *
 * This is the single auth choke point for the SPA: no per-call header wiring
 * lives in the orchestrator/KMS/IAM stores.
 *
 * Two clients: getSDKClient follows the Connection view's override, while
 * getOriginSDKClient always talks to the page's own daemon and carries the
 * session calls (sign-in, validation, sign-out).
 */

import { createClient, isWailsRuntime } from '@quicbaas/sdk';
import type { Client } from '@quicbaas/sdk';
import { getValidAccessToken } from './auth.svelte.ts';
import { connectionOverride } from './connection.svelte.ts';

// ---------------------------------------------------------------------------
// Module-level singleton
// ---------------------------------------------------------------------------

let _client: Client | null = null;
let _initPromise: Promise<Client> | null = null;

/**
 * getSDKClient returns the singleton Client, initialising it on first call.
 * Subsequent calls return the cached instance without re-initialising.
 */
export async function getSDKClient(): Promise<Client> {
  if (_client !== null) return _client;

  if (_initPromise !== null) return _initPromise;

  // token is a factory (not a static string) so the SDK re-reads the live
  // access token from the shared auth store on every request. This is what
  // makes authenticated calls work after an in-tab login without rebuilding
  // the client. getValidAccessToken also pre-emptively refreshes a token that
  // is expired or within the skew window, so a long-lived tab whose 15-minute
  // access token lapses mid-session transparently obtains a fresh one instead
  // of 401-ing (bug #20).
  const token = async (): Promise<string> => getValidAccessToken();

  _initPromise = (async (): Promise<Client> => {
    if (isWailsRuntime()) {
      _client = await createClient({ transport: 'wails', token });
    } else {
      const o = connectionOverride();
      const baseURL =
        o?.baseURL ?? (typeof window !== 'undefined' ? window.location.origin : '');
      _client = await createClient({ transport: 'rest', baseURL, token });
    }
    return _client;
  })();

  return _initPromise;
}

/**
 * activeConnection reports the transport and address getSDKClient uses:
 * 'wails' with no address under Wails, otherwise 'rest' with the persisted
 * override or the page origin.
 */
export function activeConnection(): { transport: string; address: string; overridden: boolean } {
  if (isWailsRuntime()) {
    return { transport: 'wails', address: '', overridden: false };
  }
  const o = connectionOverride();
  if (o !== null) {
    return { transport: 'rest', address: o.baseURL, overridden: true };
  }
  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  return { transport: 'rest', address: origin, overridden: false };
}

/**
 * resetSDKClient tears down the singleton so the next getSDKClient builds a
 * new client. Tests call it for a fresh client per case; the Connection view
 * calls it after the daemon address changes.
 */
export function resetSDKClient(): void {
  _client = null;
  _initPromise = null;
  _originClient = null;
  _originInit = null;
}

let _originClient: Client | null = null;
let _originInit: Promise<Client> | null = null;

/**
 * getOriginSDKClient returns the client for the page's own daemon. Sign-in,
 * registration, invite activation, session validation and sign-out use it, so
 * a session always lives on the daemon that issued it, whatever the
 * Connection view points the other calls at. It never reads the connection
 * override for its address. Under Wails, or with no override saved, both point
 * at the same daemon, so it is the getSDKClient client.
 */
export async function getOriginSDKClient(): Promise<Client> {
  if (isWailsRuntime() || connectionOverride() === null) return getSDKClient();
  if (_originClient !== null) return _originClient;
  if (_originInit !== null) return _originInit;

  const token = async (): Promise<string> => getValidAccessToken();
  _originInit = (async (): Promise<Client> => {
    const baseURL = typeof window !== 'undefined' ? window.location.origin : '';
    _originClient = await createClient({ transport: 'rest', baseURL, token });
    return _originClient;
  })();
  return _originInit;
}
