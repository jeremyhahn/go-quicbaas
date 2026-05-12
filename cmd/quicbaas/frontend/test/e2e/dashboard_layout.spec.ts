/**
 * dashboard_layout.spec.ts
 *
 * Verifies the two-dashboard layout introduced by the OVERVIEW / ORCHESTRATION
 * dashboard split:
 *
 *   #/            → System Dashboard: "System Status" heading
 *   #/dashboard   → System Dashboard: "System Status" heading
 *   #/orchestration → Orchestration Dashboard: "Orchestration Dashboard" heading
 *                     with Active Jobs / Agents / Sessions / Pending Tasks cards
 *
 * To run (requires a running quicbaasd at QUICBAAS_UI_URL):
 *   cd frontend && npx playwright test test/e2e/dashboard_layout.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Dashboard layout split', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await expect(page.locator('body')).not.toBeEmpty();
  });

  test('root route (#/) renders System Dashboard with "System Status" heading', async ({ page }) => {
    await page.goto('/ui/#/', { waitUntil: 'networkidle' });
    const heading = page.locator('h1', { hasText: 'System Status' });
    await expect(heading).toBeVisible();
  });

  test('#/dashboard renders System Dashboard with "System Status" heading', async ({ page }) => {
    await page.goto('/ui/#/dashboard', { waitUntil: 'networkidle' });
    const heading = page.locator('h1', { hasText: 'System Status' });
    await expect(heading).toBeVisible();
  });

  test('#/orchestration renders Orchestration Dashboard with stat cards', async ({ page }) => {
    await page.goto('/ui/#/orchestration', { waitUntil: 'networkidle' });

    // Main heading.
    const heading = page.locator('h1', { hasText: 'Orchestration Dashboard' });
    await expect(heading).toBeVisible();

    // Stat card labels — scoped to .stat-title to avoid sidebar/breadcrumb matches.
    await expect(page.locator('.stat-title', { hasText: 'Active Jobs' })).toBeVisible();
    await expect(page.locator('.stat-title', { hasText: 'Agents' })).toBeVisible();
    await expect(page.locator('.stat-title', { hasText: 'Sessions' })).toBeVisible();
    await expect(page.locator('.stat-title', { hasText: 'Pending Tasks' })).toBeVisible();
  });

  test('OVERVIEW sidebar "Overview" navigates to #/dashboard (system-wide)', async ({ page }) => {
    // The OVERVIEW item is now labelled "Overview" so each of the three
    // dashboard entries is uniquely addressable in the sidebar.
    const btn = page.locator('aside nav button', { hasText: 'Overview' }).first();
    await btn.click();

    await expect(page).toHaveURL(/#\/dashboard/);
    const heading = page.locator('h1', { hasText: 'System Status' });
    await expect(heading).toBeVisible();
  });

  test('ORCHESTRATION sidebar "Orchestration" navigates to #/orchestration', async ({ page }) => {
    // The ORCHESTRATION item is now labelled "Orchestration" — disambiguates
    // the OVERVIEW > Overview, ORCHESTRATION > Orchestration, CLUSTER > Cluster
    // sidebar entries that all rendered as "Dashboard" before the fix.
    const btn = page.locator('aside nav button', { hasText: 'Orchestration' }).first();
    await btn.click();

    await expect(page).toHaveURL(/#\/orchestration/);
    const heading = page.locator('h1', { hasText: 'Orchestration Dashboard' });
    await expect(heading).toBeVisible();
  });

  test('System Dashboard shows Service Health section', async ({ page }) => {
    await page.goto('/ui/#/dashboard', { waitUntil: 'networkidle' });
    await expect(page.locator('text=Service Health')).toBeVisible();
  });

  test('System Dashboard shows Connection section', async ({ page }) => {
    await page.goto('/ui/#/dashboard', { waitUntil: 'networkidle' });
    // Scope to main content area to avoid matching the "Connection" sidebar button.
    await expect(page.locator('main').getByText('Connection').first()).toBeVisible();
  });
});
