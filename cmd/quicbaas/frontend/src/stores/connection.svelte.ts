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
 * Connection state store.
 *
 * Tracks connectivity to the QuicBaaS daemon including address,
 * transport protocol, and error state.
 */

export interface ConnectionState {
  connected: boolean;
  address: string;
  transport: string;
  error: string;
}

let connected = $state(false);
let address = $state('');
let transport = $state('rest');
let error = $state('');

export const connectionStore = {
  get connected() { return connected; },
  get address() { return address; },
  get transport() { return transport; },
  get error() { return error; },
};

export function setConnected(addr: string, trans: string): void {
  connected = true;
  address = addr;
  transport = trans;
  error = '';
}

export function setDisconnected(): void {
  connected = false;
  address = '';
  error = '';
}

export function setConnectionError(err: string): void {
  connected = false;
  error = err;
}

// ---------------------------------------------------------------------------
// Persisted connection override
// ---------------------------------------------------------------------------

/**
 * ConnectionOverride is the user's choice of daemon address, set in the
 * Connection view. Only the REST transport works in a browser, so it is the
 * only transport an override can name.
 */
export interface ConnectionOverride {
  transport: 'rest';
  baseURL: string;
}

const OVERRIDE_KEY = 'quicbaas.connection.v1';

/**
 * isValidBaseURL reports whether s is an absolute http or https URL with a
 * host and nothing beyond it: no path other than '/', no query, no hash.
 */
export function isValidBaseURL(s: string): boolean {
  if (typeof s !== 'string' || s.trim() === '') return false;
  try {
    const u = new URL(s.trim());
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return false;
    if (u.hostname === '') return false;
    if (u.pathname !== '/' && u.pathname !== '') return false;
    if (u.search !== '' || u.hash !== '') return false;
    // URL() drops a bare '?' or '#'; reject them from the raw text too.
    if (s.includes('?') || s.includes('#')) return false;
    return true;
  } catch {
    return false;
  }
}

/** normalizeBaseURL returns the origin of s (scheme, host and port). */
export function normalizeBaseURL(s: string): string {
  return new URL(s.trim()).origin;
}

/**
 * connectionOverride reads the persisted override. It returns null when none
 * is stored, when storage is blocked, or when the stored value is not a valid
 * REST override.
 */
export function connectionOverride(): ConnectionOverride | null {
  let raw: string | null;
  try {
    raw = window.localStorage.getItem(OVERRIDE_KEY);
  } catch {
    return null;
  }
  if (raw === null) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed === null || typeof parsed !== 'object') return null;
    const o = parsed as { transport?: unknown; baseURL?: unknown };
    if (o.transport !== 'rest') return null;
    if (typeof o.baseURL !== 'string' || !isValidBaseURL(o.baseURL)) return null;
    return { transport: 'rest', baseURL: o.baseURL };
  } catch {
    return null;
  }
}

/** saveConnectionOverride persists o; it throws when storage is unavailable. */
export function saveConnectionOverride(o: ConnectionOverride): void {
  try {
    window.localStorage.setItem(
      OVERRIDE_KEY,
      JSON.stringify({ transport: o.transport, baseURL: o.baseURL }),
    );
  } catch {
    throw new Error('Browser storage is unavailable');
  }
}

/** clearConnectionOverride removes the persisted override, if any. */
export function clearConnectionOverride(): void {
  try {
    window.localStorage.removeItem(OVERRIDE_KEY);
  } catch {
    // Storage blocked: nothing is stored, so there is nothing to clear.
  }
}
