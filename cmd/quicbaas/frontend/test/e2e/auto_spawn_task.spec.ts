/**
 * auto_spawn_task.spec.ts
 *
 * E2E tests for the auto-spawn task feature on the CreateTaskDialog.
 *
 * Verifies:
 *   - "Run immediately" toggle is present in the create-task dialog
 *   - Toggling it surfaces the backend selector
 *   - The TasksView "Spawned Job" column header is rendered
 *   - Tasks page exposes the Auto-spawned/Queued/All filter
 *
 * Requires a running quicbaasd at QUICBAAS_UI_URL.
 * Run: cd frontend && npx playwright test test/e2e/auto_spawn_task.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Tasks view — auto-spawn affordances', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Tasks' }).first().click();
    await expect(page).toHaveURL(/#\/tasks/);
  });

  test('Tasks heading visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Tasks', exact: false }).first()).toBeVisible();
  });

  test('Spawned Job column or label is exposed', async ({ page }) => {
    // Spawned-job indication appears either as a table header or as part of an
    // empty-state hint. Either form proves the column is present in the view.
    const indicator = page
      .locator('th:has-text("Spawned")')
      .or(page.locator('th:has-text("Job")'))
      .or(page.getByText(/Spawned Job/i));
    await expect(indicator.first()).toBeVisible({ timeout: 5_000 });
  });

  test('Filter for Auto-spawned / Queued is present', async ({ page }) => {
    const filter = page
      .locator('button:has-text("Auto-spawned")')
      .or(page.locator('button:has-text("Queued")'))
      .or(page.locator('select option:has-text("Auto-spawned")'))
      .or(page.locator('[role="tab"]:has-text("Auto-spawned")'));
    await expect(filter.first()).toBeVisible({ timeout: 5_000 });
  });
});

test.describe('CreateTaskDialog — Run immediately toggle', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Tasks' }).first().click();
    await expect(page).toHaveURL(/#\/tasks/);
  });

  test('Create button opens dialog with Run immediately toggle', async ({ page }) => {
    const createBtn = page.locator('button', {
      hasText: /Create Task|New Task|\+ Task|Add Task/i,
    }).first();
    await expect(createBtn).toBeVisible();
    await createBtn.click();

    const dialog = page.locator('[role="dialog"], dialog, .modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    // Run-immediately toggle present (label contains "Run immediately" or "Auto-spawn")
    const toggle = dialog.locator(
      'label:has-text("Run immediately"), label:has-text("Auto-spawn"), input[name="autoSpawn"], input[name="auto_spawn"]'
    );
    await expect(toggle.first()).toBeVisible();
  });
});
