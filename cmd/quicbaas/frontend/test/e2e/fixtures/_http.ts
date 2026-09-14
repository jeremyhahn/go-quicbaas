/**
 * fixtures/_http.ts
 *
 * Shared admin-bearer auth + fetch helpers for the Playwright e2e fixture suite.
 *
 * All six fixture modules (api, iam, tenants, security_seed, agents_seed,
 * workflows_seed) previously reimplemented the same ~50 lines of token-cache +
 * authHeader + 401-silent-refresh logic independently. This module is the
 * single canonical home for that shared infrastructure.
 *
 * Exports:
 *   SYSTEM_TENANT_ID  — the canonical control-plane "system" tenant constant
 *   serverBase()      — derive the REST base URL from QUICBAAS_UI_URL
 *   authHeader()      — build { Authorization: Bearer <token> } from the cache
 *   apiFetch<T>()     — JSON round-trip: adds Content-Type + Accept, silently
 *                       refreshes once on 401, throws on non-2xx, returns T
 *                       (undefined for 204 No Content)
 *   authFetch()       — auth + 401-silent-refresh, returns raw Response so
 *                       callers can inspect status codes (e.g. 404) without
 *                       throwing
 *
 * Token state is module-level so it is shared across all fixture files in a
 * single Playwright worker process. A refresh in one fixture immediately
 * benefits all others — there is at most one token-refresh call per 401 per
 * process lifetime.
 */

import { readAdminTokens } from './auth.ts';

// SYSTEM_TENANT_ID is the control-plane "system" tenant that qrdb always
// provisions. The legacy "default" tenant was permanently retired by qrdb's
// tenant-isolation refactor, so all fixtures seed/tear down state under the
// system tenant.
export const SYSTEM_TENANT_ID = 'system';

// serverBase derives the REST API base URL from the same QUICBAAS_UI_URL env
// var that playwright.config.ts uses, so the two are always in sync.
export function serverBase(): string {
  const uiUrl = process.env['QUICBAAS_UI_URL'] ?? 'https://localhost:18080/ui/';
  return uiUrl.replace(/\/ui\/?$/, '');
}

// ---------------------------------------------------------------------------
// Token cache — one per worker process
// ---------------------------------------------------------------------------

let _accessToken: string | null = null;
let _refreshToken: string | null = null;

function loadAdminTokens(): void {
  if (_accessToken !== null) return;
  const t = readAdminTokens();
  _accessToken = t.access_token;
  _refreshToken = t.refresh_token;
}

// authHeader builds the Authorization header from the cached access token.
// Tokens are lazy-loaded from the file written by global-setup on first call.
export function authHeader(): Record<string, string> {
  loadAdminTokens();
  return _accessToken ? { Authorization: `Bearer ${_accessToken}` } : {};
}

// refreshAdminToken exchanges the refresh token for a fresh access token via
// the public renewal endpoint. Returns true on success.
//
// /api/v1/iam/refresh, not /api/v1/auth/refresh: on a composed node the latter
// is the xkms module's, redeeming from a different store than the ceremony that
// minted this token (go-quicbaas#221, pkg/api/rest/routes_iam.go).
async function refreshAdminToken(): Promise<boolean> {
  loadAdminTokens();
  if (!_refreshToken) return false;
  const resp = await fetch(`${serverBase()}/api/v1/iam/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ refresh_token: _refreshToken }),
  });
  if (!resp.ok) return false;
  const body = (await resp.json()) as { access_token?: string; refresh_token?: string };
  if (!body.access_token) return false;
  _accessToken = body.access_token;
  // The server ROTATES on every redemption, so the token we just presented is
  // revoked. Without this the SECOND refresh in a worker replays a dead
  // credential and trips the stolen-token detection.
  if (body.refresh_token) _refreshToken = body.refresh_token;
  return true;
}

// ---------------------------------------------------------------------------
// Fetch wrappers
// ---------------------------------------------------------------------------

// authFetch sends a request with the admin bearer token, silently refreshing
// once on 401. Returns the raw Response so callers can inspect status codes
// (e.g. 404) without throwing.
export async function authFetch(url: string, init?: RequestInit): Promise<Response> {
  const doFetch = (): Promise<Response> =>
    fetch(url, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...authHeader(),
        ...(init?.headers as Record<string, string> | undefined),
      },
    });

  let resp = await doFetch();
  if (resp.status === 401 && (await refreshAdminToken())) {
    resp = await doFetch();
  }
  return resp;
}

// apiFetch is a JSON round-trip wrapper: it adds Content-Type + Accept, silently
// refreshes once on 401, and throws on any non-2xx response. Returns undefined
// (as T) for 204 No Content responses.
//
// TLS errors are suppressed via NODE_TLS_REJECT_UNAUTHORIZED=0 which the
// Makefile / CI wrapper sets before invoking Playwright.
export async function apiFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const doFetch = (): Promise<Response> =>
    fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...authHeader(),
        ...(init?.headers as Record<string, string> | undefined),
      },
    });

  let resp = await doFetch();
  // Silently refresh once if the access token expired mid-suite.
  if (resp.status === 401 && (await refreshAdminToken())) {
    resp = await doFetch();
  }
  if (!resp.ok) {
    const body = await resp.text().catch(() => '');
    throw new Error(`API ${init?.method ?? 'GET'} ${url} failed ${resp.status}: ${body}`);
  }
  // Some endpoints return 204 No Content.
  const ct = resp.headers.get('content-type') ?? '';
  if (resp.status === 204 || !ct.includes('application/json')) {
    return undefined as T;
  }
  return resp.json() as Promise<T>;
}
