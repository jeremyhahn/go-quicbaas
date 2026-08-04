/**
 * secrets_roundtrip.spec.ts
 *
 * Operational round-trip tests for Security > Secrets (#/secrets).
 *
 * Graduates from the dialog-opens-only assertion in security_surfaces.spec.ts
 * to a full store → list → masking-verify → delete lifecycle.
 *
 * Tests (run serially; state builds across tests):
 *  1. Secrets page renders — heading and "Store Secret" button visible.
 *  2. Store Secret dialog — opens, path + data fields present.
 *  3. Secret storage — POST /api/v1/secrets/{path} returns 2xx; dialog closes.
 *  4. Secret in list — path appears as a row in the table.
 *  5. Value is masked — the raw secret value is NOT visible in the list row.
 *     (The list table shows only path, version, updated, and action buttons;
 *     the data is only revealed via the "Fetch" action which opens a separate modal.)
 *  6. Fetch reveals value — clicking "Fetch" opens the Secret value modal and
 *     shows the stored JSON in a <pre> block.
 *  7. Delete — "Delete" row action opens confirm dialog; confirm permanently
 *     destroys the secret; row disappears.
 *
 * Verified selectors (cmd/quicbaas/frontend/src/views/security/SecretsView.svelte):
 *  - Store button:       aria-label="Store secret"                (line 228)
 *  - Store dialog:       role="dialog" aria-label="Store secret dialog"  (line 317)
 *  - Path input:         id="secret-path"                         (line 329)
 *  - Data textarea:      id="secret-data"                         (line 340)
 *  - Submit button:      role="button" text="Store" in dialog     (line 358)
 *  - Path cell in list:  td.font-mono.text-xs (first column)      (line 267)
 *  - Version cell:       td.text-sm (second column)               (line 268)
 *  - Fetch button:       aria-label="Fetch secret {path}"         (line 277)
 *  - Secret value modal: role="dialog" aria-label="Secret value modal"  (line 371)
 *  - Delete button:      aria-label="Delete secret {path}"        (line 299)
 *  - Delete dialog:      role="dialog" aria-label="Delete secret confirmation" (line 460)
 *  - Confirm delete:     role="button" text="Delete" in confirm dialog  (line 472)
 *
 * REST endpoints (pkg/api/rest/server.go):
 *  POST   /api/v1/secrets/{path}            → HandleStoreSecret   (line 494 + handlers_secrets.go line 68)
 *  GET    /api/v1/secrets/                  → HandleListSecrets   (line 493)
 *  GET    /api/v1/secrets/{path}            → HandleGetSecretOrVersions (line 495)
 *  DELETE /api/v1/secrets/{path}?destroy=true → HandleDeleteSecret (line 496)
 *
 * GATING NOTE: Secrets are backed by qrdb and should work in the standard
 * no-backend daemon. If the secrets service is unavailable the view renders a
 * role="alert" error. These tests will fail with a clear assertion error on the
 * table-visible check, not a silent timeout.
 */

import { test, expect, type Page } from '@playwright/test';
import { deleteSecret } from './fixtures/security_seed.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

async function gotoSecrets(page: Page): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/secrets'; });
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });
}

// ---------------------------------------------------------------------------
// Shared test state
// ---------------------------------------------------------------------------

// Multi-segment path tests the wildcard route: POST /api/v1/secrets/e2e/roundtrip-{ts}
const SECRET_PATH = `e2e/roundtrip-${Date.now()}`;

// Unique content string used to assert value masking in the list view.
const SECRET_VALUE_UNIQUE = `e2e-secret-val-${Date.now()}`;
const SECRET_DATA_JSON = JSON.stringify({ value: SECRET_VALUE_UNIQUE });

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe.serial('Security > Secrets — store/list/mask/delete round-trip', () => {

  test.afterAll(async () => {
    // Permanently destroy all versions; deleteSecret swallows 404 / transport errors.
    await deleteSecret(SECRET_PATH);
  });

  // -------------------------------------------------------------------------
  // Test 1 — page renders
  // -------------------------------------------------------------------------
  test('1. Secrets page renders heading and Store Secret button', async ({ page }) => {
    await gotoSecrets(page);

    await expect(page.getByRole('heading', { name: 'Secrets' })).toBeVisible();
    // Two buttons match /Store secret/i when the list is empty: the header
    // button (aria-label="Store secret", accessible name = "Store secret",
    // SecretsView.svelte line 226) and the EmptyState action button
    // (actionLabel="+ Store Secret", accessible name = "+ Store Secret",
    // line 249).  Using exact: true on the literal "Store secret" string
    // matches only the header button's aria-label and not the EmptyState
    // text (which starts with "+" and uses a capital "S" in "Secret").
    await expect(
      page.getByRole('button', { name: 'Store secret', exact: true }),
    ).toBeVisible();
  });

  // -------------------------------------------------------------------------
  // Test 2 — Store Secret dialog opens with correct fields
  // -------------------------------------------------------------------------
  test('2. Store Secret dialog opens with path and data fields', async ({ page }) => {
    await gotoSecrets(page);

    await page.getByRole('button', { name: 'Store secret', exact: true }).click();

    const dialog = page.getByRole('dialog', { name: /Store secret dialog/i });
    await expect(dialog).toBeVisible();

    // Path input (id="secret-path") and data textarea (id="secret-data") must be present.
    await expect(dialog.locator('#secret-path')).toBeVisible();
    await expect(dialog.locator('#secret-data')).toBeVisible();

    // Cancel without submitting.
    await dialog.getByRole('button', { name: /Cancel/i }).click();
    await expect(dialog).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 3 — submit storage → POST /api/v1/secrets/{path} returns 2xx
  // -------------------------------------------------------------------------
  test('3. Store secret POST /api/v1/secrets/{path} returns 2xx and dialog closes', async ({ page }) => {
    await gotoSecrets(page);

    // Arm response interceptor before opening dialog so we cannot miss the request.
    const storeResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/secrets/') &&
        resp.request().method() === 'POST' &&
        // Exclude the action sub-endpoints (rotate, rollback, destroy, etc.)
        !resp.url().includes('/rotate') &&
        !resp.url().includes('/rollback') &&
        !resp.url().includes('/destroy') &&
        !resp.url().includes('/enable') &&
        !resp.url().includes('/disable'),
    );

    await page.getByRole('button', { name: 'Store secret', exact: true }).click();

    const dialog = page.getByRole('dialog', { name: /Store secret dialog/i });
    await expect(dialog).toBeVisible();

    // Fill the path (multi-segment; exercises the wildcard route).
    await dialog.locator('#secret-path').fill(SECRET_PATH);

    // Replace the default template with our test data JSON.
    await dialog.locator('#secret-data').fill(SECRET_DATA_JSON);

    await dialog.getByRole('button', { name: /^Store$/ }).click();

    const resp = await storeResponse;
    expect(
      resp.status(),
      `POST /api/v1/secrets/${SECRET_PATH} expected 2xx, got ${resp.status()}`,
    ).toBeGreaterThanOrEqual(200);
    expect(resp.status()).toBeLessThan(300);

    // Dialog must close after successful storage.
    await expect(dialog).toHaveCount(0, { timeout: 5_000 });
  });

  // -------------------------------------------------------------------------
  // Test 4 — secret path appears in list
  // -------------------------------------------------------------------------
  test('4. stored secret path appears as a row in the Secrets list', async ({ page }) => {
    await gotoSecrets(page);

    // The list table must render (not the error alert).
    const tableBody = page.locator('table tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    // The first column is td.font-mono.text-xs containing the path string.
    const pathRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: SECRET_PATH }),
    });
    await expect(
      pathRow,
      `Expected secret row for path "${SECRET_PATH}" in the Secrets list`,
    ).toBeVisible({ timeout: 8_000 });

    // Version cell (second column: td.text-sm) should be a positive integer.
    const versionCell = pathRow.locator('td.text-sm');
    await expect(versionCell).toBeVisible();
    const versionText = (await versionCell.textContent())?.trim() ?? '';
    expect(
      parseInt(versionText, 10),
      `Version for secret "${SECRET_PATH}" must be a positive integer, got "${versionText}"`,
    ).toBeGreaterThan(0);
  });

  // -------------------------------------------------------------------------
  // Test 5 — value is masked (NOT visible in the list row)
  // -------------------------------------------------------------------------
  test('5. secret value is masked — raw value not visible in list row', async ({ page }) => {
    await gotoSecrets(page);

    const tableBody = page.locator('table tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    const pathRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: SECRET_PATH }),
    });
    await expect(pathRow).toBeVisible();

    // The list table shows Path, Version, Updated, and Actions — no value column.
    // The raw secret value must not appear anywhere in the row's visible text.
    const rowText = (await pathRow.textContent()) ?? '';
    expect(
      rowText,
      'Secret value must not be shown in plain text in the list row',
    ).not.toContain(SECRET_VALUE_UNIQUE);
  });

  // -------------------------------------------------------------------------
  // Test 6 — Fetch reveals value in the Secret value modal
  // -------------------------------------------------------------------------
  test('6. Fetch action opens Secret value modal showing the stored JSON', async ({ page }) => {
    await gotoSecrets(page);

    const tableBody = page.locator('table tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    const pathRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: SECRET_PATH }),
    });
    await expect(pathRow).toBeVisible();

    // Arm the GET response interceptor before clicking Fetch.
    const fetchResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/secrets/') &&
        resp.request().method() === 'GET' &&
        !resp.url().includes('/versions'),
    );

    // Click the "Fetch" row action button: aria-label="Fetch secret {path}".
    await pathRow.locator(`button[aria-label="Fetch secret ${SECRET_PATH}"]`).click();

    const getResp = await fetchResponse;
    expect(
      getResp.status(),
      `GET /api/v1/secrets/${SECRET_PATH} expected 2xx, got ${getResp.status()}`,
    ).toBeLessThan(300);

    // Secret value modal appears: role="dialog" aria-label="Secret value modal".
    const valueModal = page.getByRole('dialog', { name: /Secret value modal/i });
    await expect(valueModal).toBeVisible({ timeout: 5_000 });

    // The value is displayed in a <pre> block as pretty-printed JSON.
    // Assert the unique sentinel string is present in the rendered output.
    await expect(valueModal.locator('pre')).toContainText(SECRET_VALUE_UNIQUE, {
      timeout: 5_000,
    });

    // Close the modal.
    await valueModal.getByRole('button', { name: /Close/i }).click();
    await expect(valueModal).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 7 — Delete removes the secret
  // -------------------------------------------------------------------------
  test('7. Delete action removes secret from list', async ({ page }) => {
    await gotoSecrets(page);

    const tableBody = page.locator('table tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    const pathRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: SECRET_PATH }),
    });
    await expect(pathRow).toBeVisible();

    // Click the Delete row action button: aria-label="Delete secret {path}".
    await pathRow.locator(`button[aria-label="Delete secret ${SECRET_PATH}"]`).click();

    const confirmDialog = page.getByRole('dialog', { name: /Delete secret confirmation/i });
    await expect(confirmDialog).toBeVisible();

    // Arm DELETE response interceptor before confirming.
    const deleteResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/secrets/') &&
        resp.request().method() === 'DELETE',
    );

    await confirmDialog.getByRole('button', { name: /^Delete$/ }).click();

    const delResp = await deleteResponse;
    expect(
      delResp.status(),
      `DELETE /api/v1/secrets/${SECRET_PATH} expected 2xx, got ${delResp.status()}`,
    ).toBeLessThan(300);

    // Row must disappear from the list.
    await expect(pathRow).toHaveCount(0, { timeout: 5_000 });
  });
});
