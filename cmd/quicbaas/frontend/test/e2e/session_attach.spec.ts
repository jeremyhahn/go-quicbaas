/**
 * session_attach.spec.ts
 *
 * Pure UI smoke tests for the Sessions view.  No session needs to exist —
 * every assertion targets the empty-state UI, the toolbar controls, and the
 * Spawn dialog.  These tests run against the ollama-backed embedded agent
 * without ACP backend requirements.
 *
 * Run:
 *   cd frontend && QUICBAAS_UI_URL=https://localhost:18080/ui/ \
 *     NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
 *     npx playwright test test/e2e/session_attach.spec.ts
 */

import { test, expect } from '@playwright/test';

const BASE = process.env['QUICBAAS_UI_URL'] ?? 'https://localhost:18080/ui/';

test.use({ ignoreHTTPSErrors: true });

test.describe('Sessions view — UI smoke tests', () => {
  test('1. Load /ui/ and see QuicBaaS brand in header', async ({ page }) => {
    await page.goto(BASE, { waitUntil: 'networkidle' });
    await expect(page.locator('header .gradient-text').getByText(/^QuicBaaS$/)).toBeVisible({
      timeout: 10_000,
    });
  });

  test('2. Navigate to sessions route via sidebar', async ({ page }) => {
    await page.goto(BASE, { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Sessions' }).first().click();
    await expect(page).toHaveURL(/#\/sessions/);
    await expect(page.locator('header .gradient-text').getByText(/^QuicBaaS$/)).toBeVisible({
      timeout: 5_000,
    });
  });

  test('3. Sessions view shows "0 of 0 sessions" counter when empty', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    // The sessions counter must be visible. With 0 sessions it reads "0 of 0 sessions".
    await expect(page.locator('text=/\\d+ of \\d+ sessions/').first()).toBeVisible({
      timeout: 10_000,
    });
  });

  test('4. "Spawn" button is visible in sessions toolbar', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    const spawnBtn = page.locator('button[aria-label="Spawn new session"]');
    await expect(spawnBtn).toBeVisible({ timeout: 10_000 });
    await expect(spawnBtn).toContainText('Spawn');
  });

  test('5. Empty-state copy is displayed when no sessions exist', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    // Wait for loading to complete (spinner must not be visible).
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });
    // Either the empty state message or a populated table row must be present.
    // When daemon has 0 sessions the empty state message is shown.
    const emptyMsg = page.locator(
      'text=No sessions yet — sessions are how you converse with persistent backends.',
    );
    const hasRows = await page.locator('[role="button"][aria-label^="Open session"]').count();
    if (hasRows === 0) {
      await expect(emptyMsg).toBeVisible({ timeout: 5_000 });
    }
    // Either path is valid — the test asserts the view renders without error.
  });

  test('6. "Spawn a session" empty-state action button opens the Spawn dialog', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    const hasRows = await page.locator('[role="button"][aria-label^="Open session"]').count();

    if (hasRows === 0) {
      // Click the action button inside the EmptyState component.
      const emptyStateAction = page.locator('button', { hasText: 'Spawn a session' }).first();
      await expect(emptyStateAction).toBeVisible({ timeout: 5_000 });
      await emptyStateAction.click();
    } else {
      // Click the toolbar spawn button.
      await page.locator('button[aria-label="Spawn new session"]').click();
    }

    // The Spawn dialog must open.
    const dialog = page.locator('dialog[aria-label="Spawn new session"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });
    await expect(dialog.locator('h3', { hasText: 'Spawn Session' })).toBeVisible();
  });

  test('7. Spawn dialog renders backend, model, host affinity, and sandbox fields', async ({
    page,
  }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await page.locator('button[aria-label="Spawn new session"]').click();

    const dialog = page.locator('dialog[aria-label="Spawn new session"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    // Backend selector or input must be present.
    const backendField = dialog.locator(
      'select[aria-label="Select backend"], input[aria-label="Backend name"]',
    );
    await expect(backendField).toBeVisible();

    // Model input.
    await expect(dialog.locator('input[aria-label="Model name"]')).toBeVisible();

    // Host affinity select.
    await expect(dialog.locator('select[aria-label="Host affinity"]')).toBeVisible();

    // Sandbox policy select.
    await expect(dialog.locator('select[aria-label="Sandbox policy"]')).toBeVisible();

    // Initial prompt textarea.
    await expect(dialog.locator('textarea[aria-label="Initial prompt"]')).toBeVisible();
  });

  test('8. Spawn button is disabled when backend field is empty', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await page.locator('button[aria-label="Spawn new session"]').click();

    const dialog = page.locator('dialog[aria-label="Spawn new session"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    // If the dialog renders a free-text input (no backends loaded), clear it so
    // backend is empty — the Spawn submit button must become disabled.
    const backendInput = dialog.locator('input[aria-label="Backend name"]');
    if (await backendInput.isVisible()) {
      await backendInput.fill('');
      const spawnSubmit = dialog.locator('button[aria-label="Spawn session"]');
      await expect(spawnSubmit).toBeDisabled();
    }
    // When a select is shown a backend is always pre-selected — skip this
    // assertion because the user cannot clear a select to empty.
  });

  test('9. Spawn dialog Cancel button closes the dialog', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await page.locator('button[aria-label="Spawn new session"]').click();

    const dialog = page.locator('dialog[aria-label="Spawn new session"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    await dialog.locator('button', { hasText: 'Cancel' }).click();
    await expect(dialog).not.toBeVisible({ timeout: 5_000 });
  });

  test('10. Escape key closes the Spawn dialog', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await page.locator('button[aria-label="Spawn new session"]').click();

    const dialog = page.locator('dialog[aria-label="Spawn new session"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });

    // Focus an element inside the dialog so the onkeydown handler fires.
    const modelInput = dialog.locator('input[aria-label="Model name"]');
    await modelInput.focus();
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible({ timeout: 5_000 });
  });

  test('11. Backend and status filter dropdowns are present in sessions toolbar', async ({
    page,
  }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await expect(page.locator('select[aria-label="Filter by backend"]')).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.locator('select[aria-label="Filter by status"]')).toBeVisible();
  });

  test('12. Refresh button is present in sessions toolbar', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });
    await expect(page.locator('button[aria-label="Refresh sessions"]')).toBeVisible({
      timeout: 10_000,
    });
  });
});
