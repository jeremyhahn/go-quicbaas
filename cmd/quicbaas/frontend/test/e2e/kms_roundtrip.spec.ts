/**
 * kms_roundtrip.spec.ts
 *
 * Operational round-trip tests for Security > KMS (#/kms).
 *
 * Graduates from the dialog-opens-only assertion in security_surfaces.spec.ts
 * to a full create → list → verify-algorithm → delete lifecycle that exercises
 * the KMS REST layer end-to-end through the browser UI.
 *
 * Tests (run serially; state builds across tests):
 *  1. KMS page renders — heading and "Create Key" button visible.
 *  2. Create Key dialog — opens, name + algorithm fields present, algorithm
 *     options include AES-256-GCM and ED25519.
 *  3. Key creation — POST /api/v1/kms/keys returns 2xx; dialog closes.
 *  4. Key in list — new key row appears with the expected algorithm cell value.
 *  5. Key deletion — per-row Delete opens confirm dialog; confirm issues
 *     DELETE /api/v1/kms/keys/{name}; row disappears.
 *
 * Verified selectors (cmd/quicbaas/frontend/src/views/security/KMSView.svelte):
 *  - Create button:    aria-label="Create key"                  (line 281)
 *  - Create dialog:    role="dialog" aria-label="Create key dialog"  (line 411)
 *  - Name input:       id="key-name"                            (line 422)
 *  - Algorithm select: id="key-algorithm"                       (line 433)
 *    Valid options: AES-256-GCM | AES-128-GCM | RSA-2048 | RSA-4096 |
 *                  ECDSA-P256 | ECDSA-P384 | ED25519
 *  - Submit button:    role="button" text="Create" in dialog    (line 473)
 *  - Key name cell:    div.font-mono.text-xs.font-medium in tbody td  (line 319)
 *  - Algorithm cell:   td.font-mono.text-xs (2nd column, no font-medium)  (line 322)
 *  - Delete per-row:   aria-label="Delete key {name}"           (line 393)
 *  - Delete dialog:    role="dialog" aria-label="Delete key confirmation"  (line 486)
 *  - Confirm Delete:   role="button" text="Delete" in confirm dialog  (line 499)
 *
 * REST endpoints (pkg/api/rest/server.go):
 *  POST   /api/v1/kms/keys        → HandleGenerateKey  (line 449)
 *  GET    /api/v1/kms/keys        → HandleListKeys     (line 452)
 *  DELETE /api/v1/kms/keys/{name} → HandleDeleteKey    (line 459)
 *
 * GATING NOTE: KMS is backed by qrdb/xkms and should work in the standard
 * no-backend daemon. If the KMS service is unavailable the view renders a
 * role="alert" error instead of the table. In that case these tests will
 * fail with a clear assertion error on the "table visible" check — not a
 * silent timeout.
 *
 * BUG WATCH (KMSView.svelte isSystemKey() filter, lines 149–153): keys where
 * algorithm='' and created_at='' are treated as system keys and hidden. If a
 * newly created key does not appear in the list, the backend may be returning
 * it without an algorithm — that is a bug in the server-side key metadata.
 */

import { test, expect, type Page } from '@playwright/test';
import { deleteKMSKey } from './fixtures/security_seed.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

async function gotoKMS(page: Page): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/kms'; });
  await page.waitForLoadState('networkidle');
  // KMSView has an internal LOAD_TIMEOUT_MS = 10 000 ms before it shows an error.
  // Wait for the spinner to clear (success or error) before asserting.
  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 12_000 });
}

// ---------------------------------------------------------------------------
// Shared test state (persists across serial tests within the describe block)
// ---------------------------------------------------------------------------

// Unique name avoids conflicts between CI runs executing concurrently.
const KEY_NAME = `e2e-kms-${Date.now()}`;
const KEY_ALGO = 'AES-256-GCM';

// Set in test 3 from the API response; used for cleanup in afterAll.
// Falls back to KEY_NAME if the response parse fails.
let createdKeyRef = '';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe.serial('Security > KMS — create/delete round-trip', () => {

  test.afterAll(async () => {
    // Best-effort cleanup — deleteKMSKey swallows 404 and transport errors.
    await deleteKMSKey(createdKeyRef || KEY_NAME);
  });

  // -------------------------------------------------------------------------
  // Test 1 — page renders
  // -------------------------------------------------------------------------
  test('1. KMS page renders heading and Create Key button', async ({ page }) => {
    await gotoKMS(page);

    await expect(page.getByRole('heading', { name: 'KMS' })).toBeVisible();
    // "Create Key" must be present regardless of whether any keys exist yet.
    await expect(
      page.getByRole('button', { name: /Create key/i }),
    ).toBeVisible();
  });

  // -------------------------------------------------------------------------
  // Test 2 — dialog opens with correct form fields
  // -------------------------------------------------------------------------
  test('2. Create Key dialog opens with name + algorithm fields', async ({ page }) => {
    await gotoKMS(page);

    await page.getByRole('button', { name: /Create key/i }).click();

    const dialog = page.getByRole('dialog', { name: /Create key dialog/i });
    await expect(dialog).toBeVisible();

    // Name input (id="key-name") and algorithm select (id="key-algorithm") are present.
    await expect(dialog.locator('#key-name')).toBeVisible();
    await expect(dialog.locator('#key-algorithm')).toBeVisible();

    // Algorithm select must contain at least the two boundary options.
    const algoSelect = dialog.locator('#key-algorithm');
    await expect(algoSelect.locator('option[value="AES-256-GCM"]')).toHaveCount(1);
    await expect(algoSelect.locator('option[value="ED25519"]')).toHaveCount(1);

    // Close without submitting to keep the server clean.
    await dialog.getByRole('button', { name: /Cancel/i }).click();
    await expect(dialog).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 3 — submit creation → POST /api/v1/kms/keys returns 2xx
  // -------------------------------------------------------------------------
  test('3. Create key POST /api/v1/kms/keys returns 2xx and dialog closes', async ({ page }) => {
    await gotoKMS(page);

    // Arm the response interceptor BEFORE opening the dialog so the request
    // cannot race past the waitForResponse setup.
    const createResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/kms/keys') &&
        resp.request().method() === 'POST' &&
        // Exclude the /import and /unwrap variants.
        !resp.url().includes('/import') &&
        !resp.url().includes('/unwrap'),
    );

    await page.getByRole('button', { name: /Create key/i }).click();

    const dialog = page.getByRole('dialog', { name: /Create key dialog/i });
    await expect(dialog).toBeVisible();

    await dialog.locator('#key-name').fill(KEY_NAME);
    await dialog.locator('#key-algorithm').selectOption(KEY_ALGO);

    // Submit the form.
    await dialog.getByRole('button', { name: /^Create$/ }).click();

    // Server must respond with 2xx.
    const resp = await createResponse;
    expect(
      resp.status(),
      `POST /api/v1/kms/keys expected 2xx, got ${resp.status()}`,
    ).toBeGreaterThanOrEqual(200);
    expect(resp.status()).toBeLessThan(300);

    // Parse the response to capture the canonical key identifier for cleanup.
    // Response shape (sdk/typescript/src/types.ts CreateKeyResponse): { key: KMSKey, timestamp: number }.
    try {
      const body = (await resp.json()) as { key?: { name?: string; id?: string } };
      createdKeyRef = body.key?.name || body.key?.id || KEY_NAME;
    } catch {
      createdKeyRef = KEY_NAME;
    }

    // Dialog must close after successful creation (showCreate = false in the component).
    await expect(dialog).toHaveCount(0, { timeout: 5_000 });
  });

  // -------------------------------------------------------------------------
  // Test 4 — new key row appears in list with correct algorithm
  // -------------------------------------------------------------------------
  test('4. created key row appears in list with non-empty algorithm cell', async ({ page }) => {
    await gotoKMS(page);

    // The list table must be visible (not the error alert or empty state).
    const tableBody = page.locator('table tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    // Locate the row by the name cell: div.font-mono.text-xs.font-medium in the first td.
    const keyRow = tableBody.locator('tr').filter({
      has: page.locator('div.font-mono.text-xs.font-medium', { hasText: KEY_NAME }),
    });
    await expect(
      keyRow,
      `Expected row for key "${KEY_NAME}" to be visible in the KMS list`,
    ).toBeVisible({ timeout: 8_000 });

    // Algorithm cell: the second column has class "font-mono text-xs" WITHOUT "font-medium".
    // BUG WATCH: if this assertion fails the key was created without an algorithm.
    const algoCell = keyRow.locator('td.font-mono.text-xs');
    await expect(algoCell).not.toBeEmpty();
    await expect(algoCell).toHaveText(KEY_ALGO);
  });

  // -------------------------------------------------------------------------
  // Test 5 — per-row Delete removes the key
  // -------------------------------------------------------------------------
  test('5. per-row Delete + confirm issues DELETE /api/v1/kms/keys/{name}', async ({ page }) => {
    await gotoKMS(page);

    const tableBody = page.locator('table tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    const keyRow = tableBody.locator('tr').filter({
      has: page.locator('div.font-mono.text-xs.font-medium', { hasText: KEY_NAME }),
    });
    await expect(keyRow).toBeVisible();

    // The delete icon button aria-label is "Delete key {key.name || key.id}".
    // Using the prefix attribute selector is robust when the display name
    // might differ from KEY_NAME (e.g. if the server uses its own canonical slug).
    await keyRow.locator('button[aria-label^="Delete key"]').click();

    // Confirm dialog appears.
    const confirmDialog = page.getByRole('dialog', { name: /Delete key confirmation/i });
    await expect(confirmDialog).toBeVisible();

    // Arm DELETE response interceptor before confirming.
    const deleteResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/kms/keys/') &&
        resp.request().method() === 'DELETE',
    );

    await confirmDialog.getByRole('button', { name: /^Delete$/ }).click();

    const delResp = await deleteResponse;
    expect(
      delResp.status(),
      `DELETE /api/v1/kms/keys/{name} expected 2xx, got ${delResp.status()}`,
    ).toBeLessThan(300);

    // Row must disappear from the list after deletion.
    await expect(keyRow).toHaveCount(0, { timeout: 5_000 });

    // Mark as cleaned up so afterAll does not make a redundant delete call.
    createdKeyRef = '';
  });
});
