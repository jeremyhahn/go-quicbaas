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
 * Proves, through the real SDK with an intercepted fetch, that the Tenant
 * Barrier view offers the tenants of the daemon its barrier client acts on
 * (the page's own) even while the Connection view points other calls at
 * another daemon with a different tenant list.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';

vi.mock('@quicbaas/sdk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@quicbaas/sdk')>();
  return {
    ...actual,
    createClient: vi.fn(actual.createClient),
    // Force the browser/REST path regardless of the jsdom runtime.
    isWailsRuntime: () => false,
  };
});
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

import TenantBarrierView from '../src/views/security/TenantBarrierView.svelte';
import { resetSDKClient } from '../src/stores/sdkClient.ts';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';
import { saveConnectionOverride } from '../src/stores/connection.svelte.ts';

const ORIGIN = window.location.origin;
const OTHER = 'https://other.example:8443';
const STATUS = `${ORIGIN}/api/v1/tenants/system/barrier/status`;

let fetchMock: ReturnType<typeof vi.fn>;

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urls(): string[] {
  return fetchMock.mock.calls.map((c) => String(c[0]));
}

beforeEach(() => {
  window.localStorage.clear();
  resetSDKClient();
  saveConnectionOverride({ transport: 'rest', baseURL: OTHER });
  setAuthenticated({ access_token: 'tok-1' });
  fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === `${OTHER}/api/v1/tenants`) {
      return json({ tenants: [{ id: 'remote-only', name: 'Remote daemon tenant' }] });
    }
    if (url === `${ORIGIN}/api/v1/tenants`) {
      return json({ tenants: [{ id: 'system', name: 'Page daemon tenant' }] });
    }
    if (url.endsWith('/api/v1/tenants/system/barrier/status')) {
      return json({ tenant_id: 'system', strategy: 'none', initialized: false, sealed: false });
    }
    return json({});
  });
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  resetSDKClient();
  setUnauthenticated();
  window.localStorage.clear();
});

describe('TenantBarrierView under a connection override', () => {
  it('lists the tenants of the daemon its barrier client acts on', async () => {
    render(TenantBarrierView);

    await waitFor(() =>
      expect(screen.getByRole('option', { name: 'Page daemon tenant' })).toBeTruthy(),
    );
    expect(screen.queryByRole('option', { name: 'Remote daemon tenant' })).toBeNull();
    await waitFor(() => expect(urls()).toContain(STATUS));
    expect(urls().some((u) => u.startsWith(OTHER))).toBe(false);
  });
});
