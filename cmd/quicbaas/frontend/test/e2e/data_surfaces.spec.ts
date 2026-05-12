/**
 * data_surfaces.spec.ts
 *
 * Smoke-level Playwright tests for the four DATA sidebar surfaces.
 * Each test navigates to the route via the sidebar, asserts the page
 * heading is visible, and checks that at least one primary CRUD button
 * is present.
 *
 * These tests require a running quicbaasd at QUICBAAS_UI_URL.
 * They do NOT perform real API calls — they only assert the UI renders.
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/data_surfaces.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// DATA surface definitions
// ---------------------------------------------------------------------------

const dataSurfaces = [
  {
    label:       'KV Store',
    hash:        '#/kv',
    heading:     'KV Store',
    crudButton:  'Put Key',
    section:     'DATA',
  },
  {
    label:       'Vectors',
    hash:        '#/vectors',
    heading:     'Vectors',
    crudButton:  'New Namespace',
    section:     'DATA',
  },
  {
    label:       'Time-Series',
    hash:        '#/timeseries',
    heading:     'Time-Series',
    crudButton:  'Write Point',
    section:     'DATA',
  },
  {
    label:       'Objects',
    hash:        '#/objects',
    heading:     'Objects',
    crudButton:  'Upload',
    section:     'DATA',
  },
] as const;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Navigate to the DATA section by clicking the sidebar button with matching
 * label, then assert the URL hash and page heading.
 */
async function navigateDataSurface(
  page: Parameters<typeof test>[1] extends (...args: infer A) => unknown ? A[0] : never,
  label: string,
  hash: string,
  heading: string,
  crudButton: string,
): Promise<void> {
  const btn = page.locator('aside nav button', { hasText: label }).first();
  await expect(btn).toBeVisible();
  await btn.click();

  await expect(page).toHaveURL(new RegExp(hash.replace('/', '\\/').replace('#', '#?')));
  await expect(page.getByRole('heading', { name: heading })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(crudButton, 'i') }).first()).toBeVisible();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('DATA sidebar surfaces — smoke', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await expect(page.locator('body')).not.toBeEmpty();
  });

  for (const surf of dataSurfaces) {
    test(`${surf.label} — heading and CRUD button visible`, async ({ page }) => {
      await navigateDataSurface(page, surf.label, surf.hash, surf.heading, surf.crudButton);
    });
  }

  test('DATA sidebar items have no "soon" badge', async ({ page }) => {
    for (const surf of dataSurfaces) {
      const btn = page.locator('aside nav button', { hasText: surf.label }).first();
      const soonBadge = btn.locator('.badge', { hasText: 'soon' });
      await expect(soonBadge).toHaveCount(0);
    }
  });

  test('KV Store breadcrumb shows QuicBaaS > Data > KV Store', async ({ page }) => {
    const btn = page.locator('aside nav button', { hasText: 'KV Store' }).first();
    await btn.click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('QuicBaaS');
    await expect(breadcrumb).toContainText('Data');
    await expect(breadcrumb).toContainText('KV Store');
  });

  test('Vectors breadcrumb shows QuicBaaS > Data > Vectors', async ({ page }) => {
    const btn = page.locator('aside nav button', { hasText: 'Vectors' }).first();
    await btn.click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Data');
    await expect(breadcrumb).toContainText('Vectors');
  });

  test('Time-Series breadcrumb shows QuicBaaS > Data > Time-Series', async ({ page }) => {
    const btn = page.locator('aside nav button', { hasText: 'Time-Series' }).first();
    await btn.click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Data');
    await expect(breadcrumb).toContainText('Time-Series');
  });

  test('Objects breadcrumb shows QuicBaaS > Data > Objects', async ({ page }) => {
    const btn = page.locator('aside nav button', { hasText: 'Objects' }).first();
    await btn.click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Data');
    await expect(breadcrumb).toContainText('Objects');
  });
});
