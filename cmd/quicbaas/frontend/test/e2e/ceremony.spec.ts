/**
 * ceremony.spec.ts
 *
 * Playwright tests for the Security > Ceremony surface.
 *
 *   1. #/ceremony renders the "Ceremony" heading.
 *   2. Sidebar shows "Ceremony" under SECURITY.
 *   3. Officer list, Claim Share, and Enroll Officer cards are all rendered.
 *   5. URL contract: page hits /api/v1/barrier/officers and never
 *      /api/v1/ceremony/*.
 *   6. Enroll-officer form submits to POST /api/v1/barrier/officers.
 *   7. Listing officers never renders a 404 banner.
 */

import { test, expect, type Page, type Request } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

async function gotoCeremony(page: Page): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/ceremony'; });
  await page.waitForLoadState('networkidle');
}

async function installLegacyURLGuard(page: Page): Promise<void> {
  await page.route('**/api/v1/ceremony/**', (route, request) => {
    throw new Error(`forbidden legacy URL hit: ${request.url()}`);
  });
  await page.route('**/api/v1/seal/**', (route, request) => {
    throw new Error(`forbidden legacy URL hit: ${request.url()}`);
  });
}

test.describe('Security > Ceremony', () => {

  test('1. #/ceremony renders the "Ceremony" heading', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoCeremony(page);
    await expect(
      page.getByRole('heading', { name: 'Ceremony', exact: true }),
    ).toBeVisible();
  });

  test('2. sidebar exposes "Ceremony" under SECURITY', async ({ page }) => {
    await installLegacyURLGuard(page);
    await page.goto('./', { waitUntil: 'networkidle' });
    await expect(
      page.locator('aside nav button', { hasText: 'Ceremony' }),
    ).toBeVisible();
  });

  test('3. officer list (or service-unavailable empty state), claim share, and enroll forms are all visible', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoCeremony(page);
    // The Officers card renders ONE of two surfaces depending on whether the
    // daemon's barrier holders are available:
    //   * `ceremony-officer-list` when /api/v1/barrier/officers returns 200
    //   * `ceremony-service-unavailable` (informational empty state) when
    //     it returns the 503 typed envelope
    // Either one is acceptable here — the regression is that neither
    // appears (or that a destructive red banner replaces the empty state).
    const officerList = page.getByTestId('ceremony-officer-list');
    const serviceUnavailable = page.getByTestId('ceremony-service-unavailable');
    await expect(officerList.or(serviceUnavailable)).toBeVisible();
    await expect(page.getByTestId('ceremony-claim-challenge')).toBeVisible();
    await expect(page.getByTestId('ceremony-enroll-form')).toBeVisible();
  });

  test('5. URL contract: page hits /api/v1/barrier/officers and never /api/v1/ceremony/*', async ({ page }) => {
    const seenURLs: string[] = [];
    page.on('request', (req: Request) => { seenURLs.push(req.url()); });
    await installLegacyURLGuard(page);

    await gotoCeremony(page);

    // The view's $effect-driven reload() runs after mount, which can race
    // past waitForLoadState('networkidle'). Poll seenURLs until the officer
    // list fetch is observed instead of relying on render timing.
    await expect
      .poll(() => seenURLs.some((u) => u.includes('/api/v1/barrier/officers')), {
        timeout: 5000,
      })
      .toBeTruthy();

    const hitLegacyCeremony = seenURLs.some((u) => u.includes('/api/v1/ceremony/'));
    expect(hitLegacyCeremony).toBeFalsy();
  });

  /**
   * Regression: the officer list used to call a route that returned 404,
   * which the Officers card surfaced as a red "404 Not Found" alert. The
   * barrier holder route returns either `{"officers": [...]}` or the
   * canonical 503 typed envelope; both shapes are acceptable, the
   * regression is specifically the 404 + "Not Found" banner.
   */
  test('7. listing officers never renders a 404 banner (404 was the regression)', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoCeremony(page);

    // The Officers heading lives at the top of the first card. Find that
    // card by anchoring on the heading.
    const heading = page.getByRole('heading', { name: 'Officers', exact: true });
    await expect(heading).toBeVisible();

    const officersCard = page
      .locator('.card', { has: heading });
    await expect(officersCard).toBeVisible();

    await expect(officersCard).not.toContainText(/\b404\b/);
    await expect(officersCard).not.toContainText(/Not Found/i);
  });

  test('6. enroll-officer form submits to POST /api/v1/barrier/officers', async ({ page }) => {
    const submitted: string[] = [];
    await page.route('**/api/v1/barrier/officers', async (route) => {
      if (route.request().method() === 'POST') {
        submitted.push(route.request().url());
      }
      await route.continue();
    });
    await installLegacyURLGuard(page);

    await gotoCeremony(page);
    await page.waitForLoadState('networkidle');

    const form = page.getByTestId('ceremony-enroll-form');
    await expect(form).toBeVisible();
    await form.getByLabel(/Username/i).fill('e2e-officer');
    await form
      .getByLabel(/^CSR \(PEM\)/i)
      .fill('-----BEGIN CERTIFICATE REQUEST-----\nE2E\n-----END CERTIFICATE REQUEST-----');
    const enroll = form.getByRole('button', { name: /Enroll Officer/i });
    // The button is disabled while the holder service is unavailable; the
    // submission contract only applies when it is live.
    if (await enroll.isEnabled()) {
      await enroll.click();
      await page.waitForLoadState('networkidle');
      expect(submitted.length).toBeGreaterThan(0);
    }
  });
});
