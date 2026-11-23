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
 * Proves, through the real SDK REST client with an intercepted fetch, that
 * requests made through getSDKClient follow the Connection view: Apply moves
 * the next authenticated request to the new daemon, Reset moves it back, and
 * a persisted override is used from the first client. It also proves the
 * session calls (validation, sign-out) stay on the page's own daemon while an
 * override points the other calls elsewhere, even after another tab changes
 * or clears that override; that another tab's change reloads this tab; and
 * that a remote daemon's 401 never renews or ends the page's session.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

vi.mock('@quicbaas/sdk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@quicbaas/sdk')>();
  return {
    ...actual,
    createClient: vi.fn(actual.createClient),
    // Force the browser/REST path regardless of the jsdom runtime.
    isWailsRuntime: () => false,
  };
});
vi.mock('../../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));
vi.mock('../../src/lib/reloadApp.ts', () => ({ reloadApp: vi.fn() }));

import ConnectionView from '../../src/views/settings/ConnectionView.svelte';
import { reloadApp } from '../../src/lib/reloadApp.ts';
import {
  getOriginSDKClient,
  getSDKClient,
  installConnectionWatch,
  resetSDKClient,
} from '../../src/stores/sdkClient.ts';
import {
  REFRESH_PATH,
  TOKEN_KEY,
  authStore,
  setAuthenticated,
  setUnauthenticated,
  validateSession,
} from '../../src/stores/auth.svelte.ts';
import { installAuthFetch, uninstallAuthFetch } from '../../src/stores/authFetch.ts';
import {
  CONNECTION_OVERRIDE_KEY,
  clearConnectionOverride,
  saveConnectionOverride,
} from '../../src/stores/connection.svelte.ts';

const ORIGIN = window.location.origin;
const OTHER = 'https://other.example:8443';
const JOBS = '/api/v1/orchestrator/jobs';

let fetchMock: ReturnType<typeof vi.fn>;
let stopWatch: (() => void) | null = null;

function urls(): string[] {
  return fetchMock.mock.calls.map((c) => String(c[0]));
}

function lastURL(): string {
  const all = urls();
  return all[all.length - 1] ?? '';
}

function lastAuthorization(): string | undefined {
  const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
  const init = (call?.[1] ?? {}) as RequestInit;
  return (init.headers as Record<string, string> | undefined)?.['Authorization'];
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  window.localStorage.clear();
  resetSDKClient();
  setUnauthenticated();
  vi.clearAllMocks();
  fetchMock = vi.fn(async (url: string) => {
    if (String(url).endsWith('/api/v1/iam/validate')) return json({ valid: true });
    return json({ jobs: [] });
  });
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  stopWatch?.();
  stopWatch = null;
  uninstallAuthFetch();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  resetSDKClient();
  setUnauthenticated();
  window.localStorage.clear();
});

describe('SDK client routing follows the Connection view', () => {
  it('SDK requests follow Apply and Reset', async () => {
    setAuthenticated({ access_token: 'tok-1' });
    const c0 = await getSDKClient();
    await c0.orchestrator.listJobs('system');
    expect(lastURL().startsWith(`${ORIGIN}${JOBS}`)).toBe(true);
    expect(lastAuthorization()).toBe('Bearer tok-1');

    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const { container } = render(ConnectionView);
    const input = container.querySelector('#connection-address') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: OTHER } });
    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(reloadApp).toHaveBeenCalledTimes(1));

    const c1 = await getSDKClient();
    expect(c1).not.toBe(c0);
    await c1.orchestrator.listJobs('system');
    expect(lastURL().startsWith(`${OTHER}${JOBS}`)).toBe(true);
    expect(lastAuthorization()).toBe('Bearer tok-1');

    const reset = await waitFor(() => screen.getByRole('button', { name: 'Reset to default' }));
    await fireEvent.click(reset);
    await waitFor(() => expect(reloadApp).toHaveBeenCalledTimes(2));

    await (await getSDKClient()).orchestrator.listJobs('system');
    expect(lastURL().startsWith(`${ORIGIN}${JOBS}`)).toBe(true);
  });

  it('a persisted override is used from the first client', async () => {
    saveConnectionOverride({ transport: 'rest', baseURL: OTHER });
    await (await getSDKClient()).orchestrator.listJobs('system');
    expect(lastURL().startsWith(`${OTHER}${JOBS}`)).toBe(true);
  });

  it("session calls stay on the page's daemon under an override", async () => {
    saveConnectionOverride({ transport: 'rest', baseURL: OTHER });
    setAuthenticated({ access_token: 'tok-2' });

    await validateSession();
    expect(urls()).toContain(`${ORIGIN}/api/v1/iam/validate`);
    expect(urls().some((u) => u.startsWith(OTHER))).toBe(false);

    await (await getOriginSDKClient()).auth.logout('tok-2');
    expect(lastURL()).toBe(`${ORIGIN}/api/v1/iam/logout`);

    expect(await getOriginSDKClient()).not.toBe(await getSDKClient());
  });

  it('the origin client ignores an override cleared by another tab', async () => {
    saveConnectionOverride({ transport: 'rest', baseURL: OTHER });
    setAuthenticated({ access_token: 'tok-3' });
    const remote = await getSDKClient();
    const origin = await getOriginSDKClient();
    expect(origin).not.toBe(remote);

    // Another tab's Reset removes the key but leaves this tab's clients alone.
    clearConnectionOverride();
    expect(await getOriginSDKClient()).toBe(origin);

    await (await getOriginSDKClient()).auth.logout('tok-3');
    expect(lastURL()).toBe(`${ORIGIN}/api/v1/iam/logout`);

    await validateSession();
    expect(urls().some((u) => u.startsWith(OTHER))).toBe(false);
  });

  it('the origin client is separate even with no override', async () => {
    expect(await getOriginSDKClient()).not.toBe(await getSDKClient());

    // Another tab applies an address; this tab has not reset its clients.
    saveConnectionOverride({ transport: 'rest', baseURL: OTHER });
    await (await getOriginSDKClient()).auth.logout('x');
    expect(lastURL()).toBe(`${ORIGIN}/api/v1/iam/logout`);
  });

  it('another tab changing the address reloads this tab', async () => {
    stopWatch = installConnectionWatch();
    const before = await getSDKClient();

    window.dispatchEvent(new StorageEvent('storage', { key: CONNECTION_OVERRIDE_KEY }));
    expect(reloadApp).toHaveBeenCalledTimes(1);
    expect(await getSDKClient()).not.toBe(before);

    window.dispatchEvent(new StorageEvent('storage', { key: 'unrelated' }));
    expect(reloadApp).toHaveBeenCalledTimes(1);
  });

  it("a remote daemon's 401 leaves the page session alone", async () => {
    let refreshCalls = 0;
    fetchMock = vi.fn(async (url: string) => {
      const u = String(url);
      if (u.includes(REFRESH_PATH)) {
        refreshCalls++;
        return json({ access_token: 'renewed' });
      }
      if (u.startsWith(`${OTHER}${JOBS}`)) {
        return new Response(JSON.stringify({ error: 'Invalid or expired token' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return json({ jobs: [] });
    });
    vi.stubGlobal('fetch', fetchMock);
    installAuthFetch();

    setAuthenticated({ access_token: 'tok-4', refresh_token: 'r-4' });
    saveConnectionOverride({ transport: 'rest', baseURL: OTHER });
    resetSDKClient();

    await expect((await getSDKClient()).orchestrator.listJobs('system')).rejects.toBeTruthy();
    expect(refreshCalls).toBe(0);
    expect(authStore.status).toBe('authenticated');
    expect(window.localStorage.getItem(TOKEN_KEY)).toBe('tok-4');
  });
});
