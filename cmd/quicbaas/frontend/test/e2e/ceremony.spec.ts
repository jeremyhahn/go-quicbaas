/**
 * ceremony.spec.ts
 *
 * Playwright tests for the new Security > Ceremony surface.
 *
 *   1. #/ceremony renders the "Ceremony" heading.
 *   2. Sidebar shows "Ceremony" under SECURITY.
 *   3. Officer list, Claim Certificate, Claim Share, and Enroll Officer
 *      cards are all rendered.
 *   4. The Kind selector exposes both cert and pubkey.
 *   5. URL contract: page hits /api/v1/init/* and never /api/v1/ceremony/*.
 *   6. Enroll-external-officer form submits to /api/v1/init/enroll-external-officer.
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

  test('3. officer list (or service-unavailable empty state), claim challenge, claim share, and enroll forms are all visible', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoCeremony(page);
    // The Pending Officers card renders ONE of two surfaces depending on
    // whether the daemon's ceremony facade is wired:
    //   * `ceremony-officer-list` when /api/v1/init/officers returns 200
    //   * `ceremony-service-unavailable` (informational empty state) when
    //     the facade is unwired and returns the 503 typed envelope
    // Either one is acceptable here — the regression is that neither
    // appears (or that a destructive red banner replaces the empty state).
    const officerList = page.getByTestId('ceremony-officer-list');
    const serviceUnavailable = page.getByTestId('ceremony-service-unavailable');
    await expect(officerList.or(serviceUnavailable)).toBeVisible();
    await expect(page.getByTestId('ceremony-claim-challenge')).toBeVisible();
    await expect(page.getByTestId('ceremony-claim-share')).toBeVisible();
    await expect(page.getByTestId('ceremony-enroll-form')).toBeVisible();
  });

  test('4. Kind selector exposes cert and pubkey', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoCeremony(page);
    const kindSelect = page.getByLabel(/^Kind$/);
    await expect(kindSelect).toBeVisible();
    const options = await kindSelect.locator('option').allTextContents();
    expect(options).toEqual(expect.arrayContaining(['cert', 'pubkey']));
  });

  test('5. URL contract: page hits /api/v1/init/officers and never /api/v1/ceremony/*', async ({ page }) => {
    const seenURLs: string[] = [];
    page.on('request', (req: Request) => { seenURLs.push(req.url()); });
    await installLegacyURLGuard(page);

    await gotoCeremony(page);

    // The view's $effect-driven reload() runs after mount, which can race
    // past waitForLoadState('networkidle'). Poll seenURLs until an
    // /api/v1/init/* fetch is observed instead of relying on render timing.
    await expect
      .poll(() => seenURLs.some((u) => u.includes('/api/v1/init/')), {
        timeout: 5000,
      })
      .toBeTruthy();

    const hitLegacyCeremony = seenURLs.some((u) => u.includes('/api/v1/ceremony/'));
    expect(hitLegacyCeremony).toBeFalsy();
  });

  /**
   * Regression: GET /api/v1/init/officers used to return 404, which the
   * Pending Officers card surfaced as a red "404 Not Found" alert. The
   * handler is now wired; either it returns `{"officers": []}` (empty
   * roster) when the ceremony facade is up, or the canonical 503 typed
   * envelope when the facade is unwired. Both shapes are acceptable;
   * the regression is specifically the 404 + "Not Found" banner.
   *
   * In the chrome-test environment the daemon boots un-initialized so
   * the ceremony facade is unwired and we expect the 503 envelope —
   * that's still the typed-error path, not a 404.
   */
  test('7. listing officers never renders a 404 banner (404 was the regression)', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoCeremony(page);

    // The Pending Officers heading lives at the top of the first card. Find
    // that card by anchoring on the heading.
    const heading = page.getByRole('heading', { name: 'Pending Officers' });
    await expect(heading).toBeVisible();

    const officersCard = page
      .locator('.card', { has: heading });
    await expect(officersCard).toBeVisible();

    // The regression was specifically a "404" status surfacing in the
    // alert envelope. The typed 503 / empty-state shapes are both
    // acceptable; only the 404 string inside the officers card is a
    // failure.
    await expect(officersCard).not.toContainText(/\b404\b/);
    // And no "Not Found" text from a chi default-404 envelope either.
    await expect(officersCard).not.toContainText(/Not Found/i);
  });

  test('6. enroll-external-officer form submits to /api/v1/init/enroll-external-officer', async ({ page }) => {
    const submittedURLs: string[] = [];
    await page.route('**/api/v1/init/enroll-external-officer', async (route) => {
      submittedURLs.push(route.request().url());
      await route.continue();
    });
    await installLegacyURLGuard(page);

    await gotoCeremony(page);
    await page.waitForLoadState('networkidle');

    const form = page.getByTestId('ceremony-enroll-form');
    await expect(form).toBeVisible();
    await form.getByLabel(/Username/i).fill('e2e-officer');
    await form.getByLabel(/Auth factor/i).fill('mtls');
    await form
      .getByLabel(/^Material \(/i)
      .fill('-----BEGIN CERTIFICATE-----\nE2E\n-----END CERTIFICATE-----');
    await form.getByRole('button', { name: /Enroll Officer/i }).click();
    await page.waitForLoadState('networkidle');

    expect(submittedURLs.some((u) => u.includes('/api/v1/init/enroll-external-officer'))).toBeTruthy();
  });
});
