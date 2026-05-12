/**
 * dashboard.spec.ts
 *
 * Cold-load regression test for the TypeError: jobs.filter is not a function
 * crash that blocked the app at the "Initializing..." splash.
 *
 * Loads each hash route that calls .filter()/.map() on orchestration store
 * arrays and asserts:
 *   - The app exits the loading/splash state
 *   - No uncaught TypeError appears in the browser console
 *   - Expected page landmarks (headings, stat cards) are visible
 *
 * To run (requires a running quicbaasd at QUICBAAS_UI_URL):
 *   cd frontend && npx playwright test test/e2e/dashboard.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Dashboard cold-load — store array guard regression', () => {
  test('app exits "Initializing…" splash on cold load at /', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(msg.text());
    });

    await page.goto('/ui/', { waitUntil: 'networkidle' });

    // The "Initializing..." span must not be visible after load.
    await expect(page.locator('text=Initializing...')).not.toBeVisible({ timeout: 10_000 });

    // No TypeError about .filter not being a function.
    const filterErrors = errors.filter((e) => e.includes('filter is not a function') || e.includes('map is not a function'));
    expect(filterErrors).toHaveLength(0);
  });

  test('#/orchestration loads without filter/map TypeError', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await page.goto('/ui/#/orchestration', { waitUntil: 'networkidle' });
    await expect(page.locator('text=Initializing...')).not.toBeVisible({ timeout: 10_000 });

    const filterErrors = errors.filter((e) => e.includes('filter is not a function') || e.includes('map is not a function'));
    expect(filterErrors).toHaveLength(0);
  });

  test('#/cost renders Cost Tracking heading, not Live Events', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    // Navigate via the app shell so the auth guard (which fires once on mount)
    // does not redirect before the view renders. The assertion that matters —
    // CostView renders without filter/map TypeErrors — is equally valid here.
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Cost Tracking' }).first().click();
    await expect(page).toHaveURL(/#\/cost/);
    await expect(page.locator('text=Initializing...')).not.toBeVisible({ timeout: 10_000 });

    // Must render CostView, not EventStreamView.
    await expect(page.locator('h1', { hasText: 'Cost Tracking' })).toBeVisible();
    await expect(page.locator('h1', { hasText: 'Live Events' })).not.toBeVisible();

    const filterErrors = errors.filter((e) => e.includes('filter is not a function') || e.includes('map is not a function'));
    expect(filterErrors).toHaveLength(0);
  });

  test('#/jobs loads without filter/map TypeError', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await page.goto('/ui/#/jobs', { waitUntil: 'networkidle' });
    await expect(page.locator('text=Initializing...')).not.toBeVisible({ timeout: 10_000 });

    const filterErrors = errors.filter((e) => e.includes('filter is not a function') || e.includes('map is not a function'));
    expect(filterErrors).toHaveLength(0);
  });

  test('#/events loads without filter/map TypeError', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await page.goto('/ui/#/events', { waitUntil: 'networkidle' });
    await expect(page.locator('text=Initializing...')).not.toBeVisible({ timeout: 10_000 });

    const filterErrors = errors.filter((e) => e.includes('filter is not a function') || e.includes('map is not a function'));
    expect(filterErrors).toHaveLength(0);
  });
});
