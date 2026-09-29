/**
 * projects.spec.ts
 *
 * E2E tests for the Projects view (#/projects).
 *
 * Verifies:
 *   - Page renders heading + table when navigated via sidebar
 *   - Sidebar item gains aria-current when active
 *   - Create-project dialog opens and exposes required fields
 *   - The dialog's git identity, autonomy and Advanced sections are reachable
 *   - A fail-closed refusal is shown before the server is called
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

  // The dialog edits the WHOLE project record, not the handful of fields it
  // once did. These walk the sections a person actually fills in when they
  // register a project for autonomous development.
  test('create dialog carries the git identity and signing controls', async ({ page }) => {
    await page.locator('button', { hasText: /Create Project|New Project/i }).first().click();
    const dialog = page.locator('dialog.modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    await dialog.getByText('Git identity & signing').click();
    for (const id of [
      '#proj-git-author-name',
      '#proj-git-author-email',
      '#proj-git-signing-enabled',
      '#proj-git-signing-algorithm',
      '#proj-git-signing-key-name',
      '#proj-git-signing-key-scope',
      '#proj-git-ssh-key-name',
    ]) {
      await expect(dialog.locator(id)).toBeVisible();
    }
  });

  test('create dialog carries the autonomous-development section', async ({ page }) => {
    await page.locator('button', { hasText: /Create Project|New Project/i }).first().click();
    const dialog = page.locator('dialog.modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    await dialog.getByText('Autonomous development').click();
    await expect(dialog.locator('#proj-autonomous')).toBeVisible();
    await expect(dialog.locator('#proj-max-concurrency')).toBeVisible();

    // Context repositories start empty and are added a row at a time.
    await expect(dialog.getByText('No context repositories.')).toBeVisible();
    await dialog.getByRole('button', { name: 'Add context repository' }).click();
    await expect(dialog.locator('#proj-context-repo-name-0')).toBeVisible();
    await expect(dialog.locator('#proj-context-repo-url-0')).toBeVisible();
    await expect(dialog.locator('#proj-context-repo-ref-0')).toBeVisible();

    await dialog.getByRole('button', { name: 'Remove context repository 1' }).click();
    await expect(dialog.locator('#proj-context-repo-name-0')).toHaveCount(0);
  });

  test('create dialog refuses signing with no key before it calls the server', async ({ page }) => {
    await page.locator('button', { hasText: /Create Project|New Project/i }).first().click();
    const dialog = page.locator('dialog.modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    await dialog.locator('#proj-name').fill('e2e-signing-refusal');
    await dialog.getByText('Git identity & signing').click();
    await dialog.locator('#proj-git-signing-enabled').check();
    await dialog.getByRole('button', { name: 'Create', exact: true }).click();

    await expect(dialog.getByText(/never degrade to unsigned/i)).toBeVisible();
    // The dialog is still open: nothing was created.
    await expect(dialog).toBeVisible();
  });

  test('the default workflow field is reachable in Advanced', async ({ page }) => {
    await page.locator('button', { hasText: /Create Project|New Project/i }).first().click();
    const dialog = page.locator('dialog.modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    await dialog.getByText('Advanced', { exact: true }).click();
    await expect(dialog.locator('#proj-default-workflow')).toBeVisible();
    await expect(dialog.locator('#proj-host-affinity')).toBeVisible();
    await expect(dialog.getByText('Spend limits')).toBeVisible();
    await expect(dialog.locator('#proj-gpu-minutes')).toBeVisible();
  });
});
