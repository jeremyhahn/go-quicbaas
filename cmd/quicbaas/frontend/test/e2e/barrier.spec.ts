/**
 * barrier.spec.ts
 *
 * Playwright tests for the renamed Security > Barrier surface.
 *
 *   1. #/barrier renders the "Barrier" heading.
 *   2. The legacy hash #/seal still resolves to the Barrier view (back-compat).
 *   3. Sidebar shows a "Barrier" item under SECURITY (no longer "Seal").
 *   4. Strategy badge is visible alongside the page card.
 *   5. URL contract: the page hits /api/v1/barrier/* and never /api/v1/seal/*.
 *   6. The unseal form (when shown) submits to /api/v1/barrier/* (canonical).
 *
 * Strategy-specific UI (shamir init/unseal, software password, auto banner)
 * is exercised by the unit tests in BarrierView.test.ts because the e2e
 * environment can't easily flip the daemon's barrier strategy mid-test.
 */

import { test, expect, type Page, type Request } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

async function gotoBarrier(page: Page, hash: string): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

// installLegacyURLGuard fails the test the moment the page issues a request
// against /api/v1/seal/* — that path is gone and must never be hit.
async function installLegacyURLGuard(page: Page): Promise<void> {
  await page.route('**/api/v1/seal/**', (route, request) => {
    throw new Error(`forbidden legacy URL hit: ${request.url()}`);
  });
}

test.describe('Security > Barrier', () => {

  test('1. #/barrier renders the "Barrier" heading', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBarrier(page, '#/barrier');
    await expect(
      page.getByRole('heading', { name: 'Barrier', exact: true }),
    ).toBeVisible();
  });

  test('2. legacy #/seal alias still routes to the Barrier surface', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBarrier(page, '#/seal');
    await expect(
      page.getByRole('heading', { name: 'Barrier', exact: true }),
    ).toBeVisible();
  });

  test('3. sidebar exposes "Barrier" (not "Seal") under SECURITY', async ({ page }) => {
    await installLegacyURLGuard(page);
    await page.goto('./', { waitUntil: 'networkidle' });
    // Exact match: a substring 'Barrier' also matches the "Tenant Barrier" item.
    const barrierBtn = page.locator('aside nav').getByRole('button', { name: 'Barrier', exact: true });
    await expect(barrierBtn).toBeVisible();
    const sealBtn = page.locator('aside nav button', { hasText: /^Seal$/ });
    await expect(sealBtn).toHaveCount(0);
  });

  test('4. status badge or strategy banner is visible after page load', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBarrier(page, '#/barrier');
    const cardOrAlert = page.locator('.card, [role="alert"]');
    await expect(cardOrAlert.first()).toBeVisible({ timeout: 10_000 });
  });

  test('5. URL contract: page hits /api/v1/barrier/status and never /api/v1/seal/*', async ({ page }) => {
    const seenURLs: string[] = [];
    page.on('request', (req: Request) => { seenURLs.push(req.url()); });
    await installLegacyURLGuard(page);

    await gotoBarrier(page, '#/barrier');

    // The view's $effect-driven reload() runs after mount, which can race
    // past waitForLoadState('networkidle'). Poll seenURLs until the status
    // fetch is observed instead of relying on render timing.
    await expect
      .poll(() => seenURLs.some((u) => u.includes('/api/v1/barrier/status')), {
        timeout: 5000,
      })
      .toBeTruthy();

    const hitLegacySeal = seenURLs.some((u) => u.includes('/api/v1/seal/'));
    expect(hitLegacySeal).toBeFalsy();
  });

  /**
   * Regression: every primary CTA across the app rendered with a coral/red
   * fill because `--color-primary` was set to the brand orange/red. Make
   * the page render and assert that the resolved background-color of a
   * btn-primary element on the Barrier surface (the "Bootstrap"/"Submit"
   * button when present, or any btn-primary on the page) lives outside
   * the brand red/orange band.
   *
   * This is a soft assertion: we accept any btn-primary element. If none
   * is rendered (e.g. the daemon is fully unsealed and shows only a
   * status banner) we skip without failing.
   */
  test('7. CTA buttons (btn-primary) do not paint with a red/coral fill', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBarrier(page, '#/barrier');

    const cta = page.locator('.btn-primary').first();
    if ((await cta.count()) === 0) {
      test.skip(true, 'no btn-primary element rendered on this Barrier state');
      return;
    }
    await expect(cta).toBeVisible({ timeout: 5_000 });

    // Browsers resolve oklch() to rgb(...) for getComputedStyle. Parse
    // the rgb triple and assert the hue lives outside the brand red
    // band by checking that R is not dominant by a large margin
    // (which is what made the old brand red read as destructive).
    const bg = await cta.evaluate((el) => getComputedStyle(el).backgroundColor);
    const m = /rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/.exec(bg);
    expect(m, `unexpected backgroundColor format: ${bg}`).not.toBeNull();
    const r = parseInt(m![1] ?? '0', 10);
    const g = parseInt(m![2] ?? '0', 10);
    const b = parseInt(m![3] ?? '0', 10);
    // Brand red lived around rgb(220, 80, 50) — R ≫ G and R ≫ B.
    // The new primary lives in the blue band — R should NOT dominate.
    expect(r - Math.max(g, b)).toBeLessThan(60);
  });

  test('6. (when sealed software) unseal form submits to /api/v1/barrier/*', async ({ page }) => {
    const submittedURLs: string[] = [];
    await page.route('**/api/v1/barrier/**', async (route) => {
      submittedURLs.push(route.request().url());
      await route.continue();
    });
    await installLegacyURLGuard(page);

    await gotoBarrier(page, '#/barrier');
    await page.waitForLoadState('networkidle');

    // The unseal form is only visible when the daemon is in software-sealed
    // state. We don't know the daemon's runtime state, so we treat this as a
    // soft assertion: if the form is present, fill + submit, otherwise pass.
    const passwordInput = page.getByLabel(/Unseal password/i);
    if (await passwordInput.count() > 0) {
      await passwordInput.fill('test-pw');
      await page.getByRole('button', { name: /Unseal/i }).click();
      await page.waitForLoadState('networkidle');
      // At least one /api/v1/barrier/* call should have been issued.
      expect(submittedURLs.some((u) => u.includes('/api/v1/barrier/'))).toBeTruthy();
    }
  });
});
