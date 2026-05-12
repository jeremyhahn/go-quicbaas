/**
 * ssh_keys.spec.ts
 *
 * Playwright smoke tests for the Security > SSH Keys route.
 *
 * Requires a running quicbaasd at QUICBAAS_UI_URL (default: http://localhost:18080/ui/).
 * The tests are deliberately lightweight — they verify route rendering and
 * basic navigation, not full API round-trips.
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/ssh_keys.spec.ts
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// Helper: navigate to a hash route under /ui/. Using relative hash avoids the
// leading-slash pitfall where page.goto('/#/x') would reset to the server root
// and miss the /ui/ prefix. waitUntil 'networkidle' lets the Svelte app mount.
async function gotoHash(page: Page, hash: string): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate((h) => {
    window.location.hash = h;
  }, hash);
  await page.waitForLoadState('networkidle');
}

// ---------------------------------------------------------------------------
// Route: Security > SSH Keys
// ---------------------------------------------------------------------------

test.describe('Security > SSH Keys — smoke', () => {
  test('SSH Keys route renders the page heading', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    await expect(page.getByRole('heading', { name: 'SSH Keys', level: 1 })).toBeVisible({
      timeout: 10_000,
    });
  });

  test('SSH Keys sidebar item is visible in the SECURITY section', async ({ page }) => {
    await gotoHash(page, '#/dashboard');
    await expect(
      page.locator('aside nav button', { hasText: 'SSH Keys' }).first(),
    ).toBeVisible();
  });

  test('clicking SSH Keys sidebar item navigates to #/ssh-keys', async ({ page }) => {
    await gotoHash(page, '#/dashboard');
    await page.locator('aside nav button', { hasText: 'SSH Keys' }).first().click();
    await expect(page).toHaveURL(/#\/ssh-keys$/);
    await expect(page.getByRole('heading', { name: 'SSH Keys', level: 1 })).toBeVisible();
  });

  test('SSH Keys route shows scope tabs', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    await expect(page.getByRole('tab', { name: /My Keys/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Tenant Keys/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Service Keys/i })).toBeVisible();
  });

  test('SSH Keys route shows Generate Key button', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    await expect(page.getByRole('button', { name: /Generate SSH key/i })).toBeVisible();
  });

  test('SSH Keys route shows Import Key button', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    await expect(page.getByRole('button', { name: /Import SSH key/i })).toBeVisible();
  });

  test('Generate Key button opens the Generate Key dialog', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    await page.getByRole('button', { name: /Generate SSH key/i }).click();
    await expect(page.getByRole('dialog', { name: /Generate SSH key dialog/i })).toBeVisible();
  });

  test('Import Key button opens the Import Key dialog', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    await page.getByRole('button', { name: /Import SSH key/i }).click();
    await expect(page.getByRole('dialog', { name: /Import SSH key dialog/i })).toBeVisible();
  });

  test('breadcrumb shows Security > SSH Keys', async ({ page }) => {
    await gotoHash(page, '#/ssh-keys');
    // Breadcrumb is a daisyUI `.breadcrumbs` div containing <ul><li>Security</li><li>SSH Keys</li></ul>.
    const breadcrumb = page.locator('.breadcrumbs').first();
    await expect(breadcrumb).toBeVisible();
    await expect(breadcrumb.locator('li', { hasText: 'Security' })).toBeVisible();
    await expect(breadcrumb.locator('li', { hasText: 'SSH Keys' })).toBeVisible();
  });
});
