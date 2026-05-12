/**
 * asset_loading.spec.ts
 *
 * Regression suite for two categories of UI path bugs:
 *
 * 1. Vite base-path bug: if the frontend is built without base: "/ui/" in
 *    vite.config.ts the asset <script> tags in index.html reference
 *    root-absolute paths (/assets/…) instead of /ui/assets/…. The daemon
 *    only serves files under /ui/*, so every asset request 404s and the
 *    Svelte app never mounts.
 *
 * 2. Wrong endpoint / method bug (Bug #4): before the SDK migration the app
 *    called legacy paths (/v1/tenants/…, /health) and called
 *    /api/v1/auth/validate via GET instead of POST. Those paths either 404
 *    or 405 — this block asserts zero such responses appear after mount.
 *
 * To run (requires a running quicbaasd):
 *   cd frontend && npx playwright test test/e2e/asset_loading.spec.ts
 *
 * Override the base URL via the QUICBAAS_UI_URL environment variable.
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Asset loading regression — base path', () => {
  test('all assets load without 4xx/5xx errors and app mounts', async ({ page }) => {
    const failedRequests: string[] = [];

    // Collect every response that is a client or server error.
    page.on('response', (response) => {
      const status = response.status();
      if (status >= 400) {
        failedRequests.push(`${status} ${response.url()}`);
      }
    });

    // Navigate to the embedded UI root and wait until the network is quiet.
    await page.goto('/ui/', { waitUntil: 'networkidle' });

    // Assert no asset requests returned an error status.
    if (failedRequests.length > 0) {
      throw new Error(
        'Assets returned error responses (check vite.config.ts base: "/ui/"):\n' +
          failedRequests.map((r) => `  ${r}`).join('\n'),
      );
    }

    // Assert the Svelte app actually mounted by checking body has children.
    // An empty body means the JS bundle never ran.
    const bodyChildCount = await page.evaluate(
      () => document.body.children.length,
    );
    expect(bodyChildCount, 'document.body has no children — Svelte app did not mount').toBeGreaterThan(0);
  });

  test('no legacy /v1/ paths or root /health are hit after mount (Bug #4 regression)', async ({ page }) => {
    // Collect every request URL with its response status.
    const legacyViolations: string[] = [];
    const methodViolations: string[] = [];

    page.on('response', (response) => {
      const url = response.url();
      const status = response.status();

      // Legacy path check: /v1/tenants/… was the pre-SDK path scheme.
      // Any request to /v1/ or to ROOT /health (not a sub-path like
      // /api/v1/orchestrator/health) is a regression.
      const pathname = new URL(url).pathname;
      if (/\/v1\/tenants\//.test(pathname) || pathname === '/health') {
        legacyViolations.push(`${status} ${url}`);
      }

      // Method check: /api/v1/auth/validate must be POST (405 = wrong method).
      if (/\/api\/v1\/auth\/validate/.test(url) && status === 405) {
        methodViolations.push(`405 (wrong method) ${url}`);
      }
    });

    await page.goto('/ui/', { waitUntil: 'networkidle' });

    if (legacyViolations.length > 0) {
      throw new Error(
        'Legacy endpoint paths detected — frontend is calling pre-SDK routes:\n' +
          legacyViolations.map((r) => `  ${r}`).join('\n'),
      );
    }

    if (methodViolations.length > 0) {
      throw new Error(
        '/api/v1/auth/validate returned 405 — must be called via POST, not GET:\n' +
          methodViolations.map((r) => `  ${r}`).join('\n'),
      );
    }
  });

  test('every GET /api/v1/orchestrator/sessions request includes tenant_id (Bug #5 regression)', async ({ page }) => {
    // Collect every request to the sessions endpoint.
    const sessionRequests: string[] = [];

    page.on('request', (request) => {
      const url = request.url();
      if (/\/api\/v1\/orchestrator\/sessions/.test(url) && request.method() === 'GET') {
        sessionRequests.push(url);
      }
    });

    await page.goto('/ui/', { waitUntil: 'networkidle' });

    // If the app issued no sessions requests we cannot assert anything —
    // but if it did, every one must carry tenant_id.
    for (const url of sessionRequests) {
      expect(
        new URL(url).searchParams.has('tenant_id'),
        `GET ${url} is missing tenant_id query param — tenant isolation leak`,
      ).toBe(true);
    }
  });

  test('index.html uses relative asset paths that work under both / and /ui/ (Bug #7 regression)', async ({ page }) => {
    // Fetch index.html directly and inspect the raw HTML. Asset references
    // must be RELATIVE (./assets/foo) so the same bundle works under the
    // daemon's /ui/ mount AND the Wails desktop's / mount. An absolute
    // /ui/ prefix was Bug #7 — it blanked the Wails window because Wails
    // serves its embedded assets at root, not under /ui/.
    const response = await page.request.get('/ui/');
    expect(response.status()).toBe(200);

    const html = await response.text();

    // Match every src="..." and href="..." that references an asset.
    const assetRefs = [...html.matchAll(/(?:src|href)="([^"]+\/assets\/[^"]+)"/g)].map(
      (m) => m[1],
    );

    expect(
      assetRefs.length,
      'No asset references found in index.html — unexpected page structure',
    ).toBeGreaterThan(0);

    for (const ref of assetRefs) {
      // Must NOT start with "/" (absolute). Must not be an http(s):// URL
      // except for known CDN cases (we have none).
      expect(
        ref.startsWith('/'),
        `Asset path "${ref}" is absolute (leading /). Must be relative (./…) so it works under both the daemon /ui/ mount and Wails /. Rebuild with base: "./" in vite.config.ts.`,
      ).toBe(false);
    }
  });
});
