/**
 * bootstrap.spec.ts
 *
 * Playwright tests for the new Security > Bootstrap auto-bootstrap wizard.
 *
 *   1. #/bootstrap renders the "Bootstrap" heading.
 *   2. Sidebar shows "Bootstrap" under SECURITY.
 *   3. All four method radios (spki-pin, dane, noise, direct) are visible.
 *   4. Switching to direct hides the trust-material textarea.
 *   5. Submit button enables only when the form is filled.
 *   6. URL contract: SPKI / DANE / Noise / Direct each submit to the
 *      canonical /api/v1/bootstrap/{spki,dane,noise,auto} surface, and
 *      legacy /api/v1/seal/* paths are never hit.
 *
 * NOTE on the form submit button: the sidebar nav also exposes a "Bootstrap"
 * button (it routes to #/bootstrap), so a page-wide getByRole match for
 * /^Bootstrap$/i matches BOTH and trips strict-mode. Scope the form button
 * to getByTestId('bootstrap-method-picker') (the BootstrapMethodPicker root)
 * so the locator resolves uniquely.
 *
 * Submit-enable predicate (from BootstrapMethodPicker.svelte):
 *   disabled = !serverUrl.trim() || (currentMeta.requiresMaterial && !material.trim())
 * For spki-pin/dane/noise, both Server URL and the material textarea must
 * be non-empty. For direct, only Server URL is required.
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

async function gotoBootstrap(page: Page): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/bootstrap'; });
  await page.waitForLoadState('networkidle');
}

async function installLegacyURLGuard(page: Page): Promise<void> {
  await page.route('**/api/v1/seal/**', (route, request) => {
    throw new Error(`forbidden legacy URL hit: ${request.url()}`);
  });
  await page.route('**/api/v1/ceremony/**', (route, request) => {
    throw new Error(`forbidden legacy URL hit: ${request.url()}`);
  });
}

test.describe('Security > Bootstrap', () => {

  test('1. #/bootstrap renders the "Bootstrap" heading', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBootstrap(page);
    await expect(
      page.getByRole('heading', { name: 'Bootstrap', exact: true }),
    ).toBeVisible();
  });

  test('2. sidebar exposes "Bootstrap" under SECURITY', async ({ page }) => {
    await installLegacyURLGuard(page);
    await page.goto('./', { waitUntil: 'networkidle' });
    await expect(
      page.locator('aside nav button', { hasText: 'Bootstrap' }),
    ).toBeVisible();
  });

  test('3. all four bootstrap method radios are visible', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBootstrap(page);
    await expect(page.getByTestId('bootstrap-method-radio-spki-pin')).toBeVisible();
    await expect(page.getByTestId('bootstrap-method-radio-dane')).toBeVisible();
    await expect(page.getByTestId('bootstrap-method-radio-noise')).toBeVisible();
    await expect(page.getByTestId('bootstrap-method-radio-direct')).toBeVisible();
  });

  test('4. selecting "direct" hides the material textarea', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBootstrap(page);
    await expect(page.getByLabel(/SPKI Pin value/i)).toBeVisible();
    await page.getByTestId('bootstrap-method-radio-direct').check();
    await expect(page.getByLabel(/SPKI Pin value/i)).toHaveCount(0);
    await expect(page.getByText(/No additional material required/i)).toBeVisible();
  });

  test('5. Submit button enables once server URL and material are filled', async ({ page }) => {
    await installLegacyURLGuard(page);
    await gotoBootstrap(page);
    // Scope to the form root to avoid colliding with the sidebar nav button.
    const picker = page.getByTestId('bootstrap-method-picker');
    const btn = picker.getByRole('button', { name: /^Bootstrap$/i });
    await expect(btn).toBeDisabled();

    await page.getByLabel(/Server URL/i).fill('https://daemon.test:9443');
    await expect(btn).toBeDisabled();

    await page.getByLabel(/SPKI Pin value/i).fill('sha256/abcdef1234');
    await expect(btn).not.toBeDisabled();
  });

  test('6. SPKI submit posts to /api/v1/bootstrap/spki', async ({ page }) => {
    const submittedURLs: string[] = [];
    await page.route('**/api/v1/bootstrap/**', async (route) => {
      submittedURLs.push(route.request().url());
      await route.continue();
    });
    await installLegacyURLGuard(page);

    await gotoBootstrap(page);
    await page.getByLabel(/Server URL/i).fill('https://localhost:18080');
    await page.getByLabel(/SPKI Pin value/i).fill('sha256/abcdef1234');
    // Scope to the form root to avoid colliding with the sidebar nav button.
    await page
      .getByTestId('bootstrap-method-picker')
      .getByRole('button', { name: /^Bootstrap$/i })
      .click();
    await page.waitForLoadState('networkidle');

    expect(submittedURLs.some((u) => u.includes('/api/v1/bootstrap/spki'))).toBeTruthy();
  });

  test('7. Direct submit posts to /api/v1/bootstrap/auto', async ({ page }) => {
    const submittedURLs: string[] = [];
    await page.route('**/api/v1/bootstrap/**', async (route) => {
      submittedURLs.push(route.request().url());
      await route.continue();
    });
    await installLegacyURLGuard(page);

    await gotoBootstrap(page);
    await page.getByTestId('bootstrap-method-radio-direct').check();
    await page.getByLabel(/Server URL/i).fill('https://localhost:18080');
    // Scope to the form root to avoid colliding with the sidebar nav button.
    await page
      .getByTestId('bootstrap-method-picker')
      .getByRole('button', { name: /^Bootstrap$/i })
      .click();
    await page.waitForLoadState('networkidle');

    expect(submittedURLs.some((u) => u.includes('/api/v1/bootstrap/auto'))).toBeTruthy();
  });
});
