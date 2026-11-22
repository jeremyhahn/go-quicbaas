/**
 * connection_view.spec.ts
 *
 * Settings > Connection: the view shows the REST transport and the page
 * origin, validates the daemon address inline, resets a saved override,
 * refuses to save another origin the page cannot reach (the page's content
 * security policy blocks it), and its sidebar entry is no longer marked "soon".
 *
 * Runs in the chromium-no-backend project (daemon at QUICBAAS_UI_URL).
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

const KEY = 'quicbaas.connection.v1';

async function navigateTo(page: Page, hash: string): Promise<void> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

async function storedOverride(page: Page): Promise<string | null> {
  return page.evaluate((k: string) => window.localStorage.getItem(k), KEY);
}

test.describe('Connection view', () => {
  test.afterEach(async ({ page }) => {
    await page.evaluate((k: string) => window.localStorage.removeItem(k), KEY).catch(() => {});
  });

  test('shows the current transport and the page origin', async ({ page }) => {
    await navigateTo(page, '#/connection');
    await expect(page.getByRole('heading', { name: 'Connection' })).toBeVisible({ timeout: 8_000 });

    const origin = await page.evaluate(() => window.location.origin);
    await expect(page.locator('#connection-address')).toHaveValue(origin);
    await expect(page.getByTestId('connection-transport')).toHaveText('rest');
  });

  test('an invalid address shows the inline error and disables Apply', async ({ page }) => {
    await navigateTo(page, '#/connection');
    await page.locator('#connection-address').fill('not a url');
    await expect(page.getByTestId('connection-address-error')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Apply' })).toBeDisabled();
  });

  test('Reset to default removes a saved override', async ({ page }) => {
    await navigateTo(page, '#/connection');
    const origin = await page.evaluate(() => window.location.origin);

    // Seed an override on the page's own origin: the page can reach it.
    await page.evaluate(
      ([k, v]: [string, string]) => window.localStorage.setItem(k, v),
      [KEY, JSON.stringify({ transport: 'rest', baseURL: origin })] as [string, string],
    );
    await navigateTo(page, '#/connection');
    await expect(page.getByTestId('connection-source')).toHaveText('Custom');

    await page.getByRole('button', { name: 'Reset to default' }).click();
    await expect.poll(async () => storedOverride(page)).toBeNull();
    await expect(page.getByTestId('connection-source')).toHaveText('Default');
  });

  test('another origin the page cannot reach is not saved', async ({ page }) => {
    await navigateTo(page, '#/connection');
    page.once('dialog', (d) => d.accept());
    await page.locator('#connection-address').fill('https://other.example:8443');
    await page.getByRole('button', { name: 'Apply' }).click();
    await expect(page.getByTestId('connection-apply-error')).toBeVisible({ timeout: 10_000 });
    expect(await storedOverride(page)).toBeNull();
    await expect(page.getByTestId('connection-source')).toHaveText('Default');
  });

  test('the Connection sidebar item has no "soon" badge', async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    const btn = page.locator('aside nav button', { hasText: 'Connection' }).first();
    await expect(btn.locator('.badge', { hasText: 'soon' })).toHaveCount(0);
  });
});
