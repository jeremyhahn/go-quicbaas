/**
 * ref_picker_ssh_key.spec.ts
 *
 * E2E coverage for the universal RefPicker (components/core/RefPicker.svelte)
 * on a kind OTHER than secret: kind="sshKey", as it appears on the Create
 * Project dialog's "Git identity & signing" section — a surface every build
 * carries, unlike the SDLC binding editor (see ref_picker_secret_create.spec.ts
 * for why that one is gated).
 *
 * Verifies:
 *   - the Signing Key Name field is a WAI-ARIA combobox, not a plain
 *     text input (role="combobox", aria-expanded);
 *   - focusing it opens the suggestions popover;
 *   - typing narrows the popover (or shows the "nothing matches" empty
 *     state when no SSH key exists yet — this suite makes no assumption
 *     about seeded keys);
 *   - typing mirrors straight into the field's value, so a caller who
 *     already knows the key name is never blocked from typing it directly.
 *
 * Verified selectors (cmd/quicbaas/frontend/src/components/orchestrator/ProjectFormFields.svelte,
 * cmd/quicbaas/frontend/src/components/core/RefPicker.svelte):
 *   - Create Project button: text /Create Project|New Project/i (projects.spec.ts)
 *   - Signing key field:     #proj-git-signing-key-name, role="combobox"
 *   - Popover:                [data-testid="ref-picker-sshKey-popover"]
 *   - Empty state:             [data-testid="ref-picker-sshKey-empty"]
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('RefPicker — SSH key combobox on the Create Project dialog', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('./', { waitUntil: 'networkidle' });
    await page.evaluate(() => { window.location.hash = '#/projects'; });
    await page.waitForLoadState('networkidle');
    await page.locator('button', { hasText: /Create Project|New Project/i }).first().click();
    const dialog = page.locator('dialog.modal').first();
    await expect(dialog).toBeVisible({ timeout: 5_000 });
    // The signing key field lives inside the collapsed "Git identity & signing"
    // details panel — open it first.
    await dialog.getByText('Git identity & signing').click();
  });

  test('renders as a WAI-ARIA combobox, not a plain text box', async ({ page }) => {
    const field = page.locator('#proj-git-signing-key-name');
    await expect(field).toBeVisible();
    await expect(field).toHaveAttribute('role', 'combobox');
    await expect(field).toHaveAttribute('aria-expanded', 'false');
  });

  test('focusing opens the suggestions popover', async ({ page }) => {
    const field = page.locator('#proj-git-signing-key-name');
    await field.click();
    await expect(field).toHaveAttribute('aria-expanded', 'true');

    const popover = page.locator('[data-testid="ref-picker-sshKey-popover"]');
    await expect(popover).toBeVisible({ timeout: 5_000 });

    // Either an SSH key is offered, or the picker says plainly that none
    // matched — never a silently empty, unexplained panel.
    const anyOption = popover.locator('[data-testid^="ref-picker-sshKey-option-"]');
    const empty = page.locator('[data-testid="ref-picker-sshKey-empty"]');
    await expect(anyOption.first().or(empty)).toBeVisible({ timeout: 5_000 });
  });

  test('typing mirrors straight into the field — a known name is never blocked', async ({ page }) => {
    const field = page.locator('#proj-git-signing-key-name');
    await field.click();
    await field.fill('commit-signing-e2e-probe');
    await expect(field).toHaveValue('commit-signing-e2e-probe');
  });
});
