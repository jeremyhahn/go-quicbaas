/**
 * workflows.spec.ts
 *
 * E2E tests for the Workflows page (#/workflows).
 *
 * Requires a running quicbaasd at QUICBAAS_UI_URL.
 * Run: cd frontend && npx playwright test test/e2e/workflows.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Workflows page', () => {
  test.beforeEach(async ({ page }) => {
    // Navigate via the app shell so the auth guard (which fires once on mount)
    // does not redirect before the Workflows view renders.
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Workflows' }).first().click();
    await expect(page).toHaveURL(/#\/workflows/);
  });

  test('page heading "Workflows" is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Workflows' })).toBeVisible();
  });

  test('sidebar item "Workflows" is active', async ({ page }) => {
    const workflowBtn = page.locator('aside nav button', { hasText: 'Workflows' }).first();
    await expect(workflowBtn).toBeVisible();
    await expect(workflowBtn).toHaveAttribute('aria-current', 'page');
  });

  test('breadcrumb contains "Workflows"', async ({ page }) => {
    const breadcrumb = page.locator('.breadcrumbs li').last();
    await expect(breadcrumb).toContainText('Workflows');
  });

  test('Create Workflow button is present', async ({ page }) => {
    await expect(
      page.getByRole('button', { name: /Create Workflow/i }),
    ).toBeVisible();
  });

  test('clicking Create Workflow opens the definition panel', async ({ page }) => {
    await page.getByRole('button', { name: /Create Workflow/i }).click();
    await expect(page.getByLabel(/Workflow Name/i)).toBeVisible();
  });
});
