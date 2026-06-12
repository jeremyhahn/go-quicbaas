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
  status = 'authenticated';
}

/** Clear persisted tokens and flip the in-tab status to 'unauthenticated'. */
export function setUnauthenticated(): void {
  if (typeof window !== 'undefined') {
    window.localStorage.removeItem(TOKEN_KEY);
    window.localStorage.removeItem(REFRESH_TOKEN_KEY);
  }
  status = 'unauthenticated';
}

/** Force the status to a known value without touching token storage. */
export function setAuthStatus(next: AuthStatus): void {
  status = next;
}

/**
 * Validate the persisted token against the server and set the status
 * accordingly. With no token we are unauthenticated; a server-rejected or
 * unreachable token also resolves to 'unauthenticated' so protected UI is
 * never shown without a confirmed session.
 */
export async function validateSession(): Promise<void> {
  const token = readAuthToken();
  if (!token) {
    status = 'unauthenticated';
    return;
  }
  try {
    const client = await getSDKClient();
    const ok = await client.auth.authValidate(token);
    status = ok ? 'authenticated' : 'unauthenticated';
  } catch {
    status = 'unauthenticated';
  }
}
