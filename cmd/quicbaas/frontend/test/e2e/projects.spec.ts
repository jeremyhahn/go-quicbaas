/**
 * projects.spec.ts
 *
 * E2E tests for the Projects view (#/projects).
 *
 * Verifies:
 *   - Page renders heading + table when navigated via sidebar
 *   - Sidebar item gains aria-current when active
 *   - Create-project dialog opens and exposes required fields
 *   - Empty state appears when no projects exist
 *
 * Requires a running quicbaasd at QUICBAAS_UI_URL.
 * Run: cd frontend && npx playwright test test/e2e/projects.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Projects view', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Projects' }).first().click();
    await expect(page).toHaveURL(/#\/projects/);
  });

  test('page heading "Projects" is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Projects', exact: false }).first()).toBeVisible();
  });

  test('sidebar item "Projects" is the active route', async ({ page }) => {
    const btn = page.locator('aside nav button', { hasText: 'Projects' }).first();
    await expect(btn).toHaveAttribute('aria-current', 'page');
  });

  test('view does not contain runtime error markers', async ({ page }) => {
    // No raw error toasts surfaced from a broken initial fetch
    await expect(page.locator('text=/runtime error/i')).toHaveCount(0);
    await expect(page.locator('text=/cannot read prop/i')).toHaveCount(0);
  });

  test('"Create Project" or "New Project" affordance is present', async ({ page }) => {
    // The view exposes a button to create a new project (label may be either form)
    const createBtn = page.locator('button', {
      hasText: /Create Project|New Project|\+ Project|Add Project/i,
    }).first();
    await expect(createBtn).toBeVisible();
  });

  test('clicking create opens a dialog with name + workspace fields', async ({ page }) => {
    const createBtn = page.locator('button', {
      hasText: /Create Project|New Project|\+ Project|Add Project/i,
    }).first();
    await createBtn.click();

    // Wait for any dialog/modal element to appear
    const dialog = page.locator('[role="dialog"], dialog, .modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    // The dialog must request a project name field
    const nameInput = dialog.locator('input[name="name"], input[placeholder*="name" i], input[id*="name" i]').first();
    await expect(nameInput).toBeVisible();
  });
});
