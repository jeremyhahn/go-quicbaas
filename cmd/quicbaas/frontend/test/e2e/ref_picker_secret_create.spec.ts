/**
 * ref_picker_secret_create.spec.ts
 *
 * E2E coverage for the universal RefPicker's inline CREATE flow
 * (components/core/RefPicker.svelte, kind="secret"), on the surface the
 * owner named directly: the SDLC binding editor's forge Token Secret Ref
 * field (SdlcBindingFields.svelte).
 *
 * GATING NOTE: the SDLC module's binding editor only renders when the
 * module answers the project's binding probe — SdlcBindingFields.svelte's
 * own header comment says "hides the whole section when the module is not
 * running on this node." A standard no-backend e2e daemon may not carry it.
 * Every test below checks for the section and calls test.skip(true, …) with
 * a clear reason when it is absent, the same defensive pattern
 * barrier.spec.ts already uses for a CTA that only sometimes renders —
 * rather than failing a suite that correctly reflects a node with the
 * module off.
 *
 * Verifies, when the module is present:
 *   - typing an unmatched path into the Token Secret Ref field offers a
 *     "Create secret `<path>`" row;
 *   - choosing it opens an inline panel with a Value field, never a
 *     separate page;
 *   - submitting issues POST /api/v1/secrets/{path} and selects the new
 *     secret, closing the panel with the field showing the created path.
 *
 * Verified selectors (SdlcBindingFields.svelte, RefPicker.svelte):
 *   - Token field:   #edit-proj-sdlc-token-secret-ref, role="combobox"
 *   - Create row:    [data-testid="ref-picker-secret-create-row"]
 *   - Create panel:  [data-testid="ref-picker-secret-create-panel"]
 *   - Value field:   label "Value"
 *   - Create button: role="button" name /^Create$/ inside the panel
 */

import { test, expect, type Page } from '@playwright/test';
import { authFetch, serverBase } from './fixtures/_http.ts';
import { deleteSecret } from './fixtures/security_seed.ts';

test.use({ ignoreHTTPSErrors: true });

const PROJECT_NAME = `e2e-refpicker-secret-${Date.now()}`;
const SECRET_PATH = `sdlc/reviewer/e2e-refpicker-${Date.now()}/token`;
let projectID = '';

// ---------------------------------------------------------------------------
// Project fixture — created once via the REST API (cheaper and more direct
// than driving the Create Project dialog), torn down in afterAll.
// ---------------------------------------------------------------------------

test.beforeAll(async () => {
  const resp = await authFetch(`${serverBase()}/api/v1/projects`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tenant_id: 'system', name: PROJECT_NAME }),
  });
  if (resp.ok) {
    const body = (await resp.json()) as { project?: { id?: string } };
    projectID = body.project?.id ?? '';
  }
});

test.afterAll(async () => {
  await deleteSecret(SECRET_PATH);
  if (projectID !== '') {
    await authFetch(`${serverBase()}/api/v1/projects/${projectID}?tenant_id=system`, { method: 'DELETE' });
  }
});

async function openBindingSection(page: Page): Promise<boolean> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/projects'; });
  await page.waitForLoadState('networkidle');

  const card = page.locator('.card', { hasText: PROJECT_NAME }).first();
  if ((await card.count()) === 0) return false;
  await card.getByRole('button', { name: 'Edit project' }).click();

  const dialog = page.locator('dialog.modal').first();
  await expect(dialog).toBeVisible({ timeout: 5_000 });

  const bindingToggle = dialog.getByText('Issue-driven delivery (SDLC)');
  if ((await bindingToggle.count()) === 0) return false;
  await bindingToggle.click();

  const enable = dialog.locator('#edit-proj-sdlc-enabled');
  if ((await enable.count()) === 0) return false;
  if (!(await enable.isChecked())) await enable.check();

  const tokenField = dialog.locator('#edit-proj-sdlc-token-secret-ref');
  return (await tokenField.count()) > 0;
}

test.describe('RefPicker — secret create flow on the SDLC forge Token Secret Ref field', () => {
  test('typing an unmatched path offers a "Create secret" row', async ({ page }) => {
    if (!(await openBindingSection(page))) {
      test.skip(true, 'SDLC module is not mounted on this node — binding section never rendered');
      return;
    }

    const field = page.locator('#edit-proj-sdlc-token-secret-ref');
    await field.click();
    await field.fill(SECRET_PATH);

    const createRow = page.locator('[data-testid="ref-picker-secret-create-row"]');
    await expect(createRow).toBeVisible({ timeout: 5_000 });
    await expect(createRow).toContainText(SECRET_PATH);
  });

  test('choosing it opens an inline panel and creates the secret without leaving the form', async ({ page }) => {
    if (!(await openBindingSection(page))) {
      test.skip(true, 'SDLC module is not mounted on this node — binding section never rendered');
      return;
    }

    const field = page.locator('#edit-proj-sdlc-token-secret-ref');
    await field.click();
    await field.fill(SECRET_PATH);

    const createRow = page.locator('[data-testid="ref-picker-secret-create-row"]');
    await expect(createRow).toBeVisible({ timeout: 5_000 });
    await createRow.click();

    const panel = page.locator('[data-testid="ref-picker-secret-create-panel"]');
    await expect(panel).toBeVisible({ timeout: 5_000 });
    await panel.getByLabel(/Value/i).fill('ghp_e2e_probe_token');

    const storeResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/secrets/') &&
        resp.request().method() === 'POST',
    );
    await panel.getByRole('button', { name: /^Create$/ }).click();

    const resp = await storeResponse;
    expect(resp.status(), `POST /api/v1/secrets/... expected 2xx, got ${resp.status()}`).toBeLessThan(300);

    // The panel closes and the field now shows the created path — created,
    // never a page navigation.
    await expect(panel).toHaveCount(0, { timeout: 5_000 });
    await expect(field).toHaveValue(SECRET_PATH);
  });
});
