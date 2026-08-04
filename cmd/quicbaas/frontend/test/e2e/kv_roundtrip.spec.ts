/**
 * kv_roundtrip.spec.ts
 *
 * Operational round-trip tests for Data > KV Store (#/kv).
 *
 * This is the most important data-plane write test in the suite: it exercises
 * the full KV put → list → detail-view → delete lifecycle through the browser
 * UI and verifies the underlying REST layer is wired end-to-end.
 *
 * The existing data_surfaces.spec.ts only asserts that the "Put Key" button is
 * visible — it never executes a write. This suite graduates that to real API
 * calls.
 *
 * Tests (run serially; state builds across tests):
 *  1. KV Store page renders — heading, entity-type input, filter input, Put Key button.
 *  2. Put Key dialog opens — key input and value textarea present.
 *  3. Key PUT round-trip — PUT /api/v1/kv/default/{key} returns 2xx; dialog closes.
 *  4. Key appears in list — key cell visible in the table with a non-empty preview.
 *  5. Detail modal — clicking the eye icon opens the "Key detail modal" showing
 *     the full JSON value including the unique sentinel string.
 *  6. Delete round-trip — per-row delete icon opens confirm dialog; confirm issues
 *     DELETE /api/v1/kv/default/{key}; row disappears.
 *
 * NOTE on dialog element type: KVStoreView.svelte uses native HTML <dialog>
 * elements (not <div role="dialog">) for the Put, Edit, Delete confirm, and
 * Detail modals. Playwright's getByRole('dialog') matches both element types,
 * and the aria-label is on the <dialog> element itself. The selectors below
 * are verified against the Svelte template.
 *
 * Verified selectors (cmd/quicbaas/frontend/src/views/data/KVStoreView.svelte):
 *  - Put Key button:       role="button" text /Put Key/ (no aria-label; line 195)
 *  - Put dialog:           <dialog aria-label="Put key-value dialog">      (line 306)
 *  - Key input:            input[type="text"].font-mono inside Put dialog  (line 319)
 *    (no id on this element — located by type within the dialog)
 *  - Value textarea:       textarea.textarea.textarea-bordered inside dialog (line 326)
 *    (no id — located by element type within the dialog)
 *  - Submit "Store":       role="button" text /^Store$/ in dialog          (line 335)
 *  - Cancel:               role="button" text /Cancel/ in dialog           (line 334)
 *  - Key table:            table.table.table-zebra tbody (line 253)
 *  - Key cell:             td.font-mono.text-xs (first column)             (line 264)
 *  - Preview cell:         td.text-xs (truncated JSON preview, 2nd column) (line 265)
 *  - Eye (detail) button:  aria-label="View {entry.key}"                   (line 276)
 *  - Key detail dialog:    <dialog aria-label="Key detail modal">          (line 395)
 *  - Detail pre block:     pre inside detail dialog                        (line 399)
 *  - Delete button:        aria-label="Delete {entry.key}"                 (line 288)
 *  - Delete confirm dialog:<dialog aria-label="Delete key confirmation">   (line 375)
 *  - Confirm delete:       role="button" text /^Delete$/ in confirm dialog (line 383)
 *
 * REST endpoints (pkg/api/rest/server.go):
 *  PUT  /api/v1/kv/{type}/{key}  → HandleKVPut   (line 435; POST also accepted, line 436)
 *  GET  /api/v1/kv/{type}/       → HandleKVList  (line 433)
 *  GET  /api/v1/kv/{type}/{key}  → HandleKVGet   (line 434)
 *  DELETE /api/v1/kv/{type}/{key}→ HandleKVDelete (line 437)
 *
 * SDK KV path (sdk/typescript/src/rest.ts):
 *  kvPut:    PUT  /api/v1/kv/{encodeURIComponent(type)}/{encodeURIComponent(key)}  (line 914)
 *  kvList:   GET  /api/v1/kv/{encodeURIComponent(type)}                            (line 884)
 *  kvDelete: DELETE /api/v1/kv/{...}/{...}                                         (line 924)
 *
 * GATING NOTE: KV Store is backed by qrdb and should work in the standard
 * no-backend daemon. If the service is unavailable the view renders an
 * alert-error div; these tests will fail with a clear assertion rather than a
 * silent timeout.
 *
 * BUG WATCH: The entity-type input defaults to "default". The Put dialog form
 * submits against that entity type. If fetchEntries() after a successful PUT
 * does not show the new key, the list may be fetching a different entity type
 * than what was used in the PUT. This would be a state-sync bug in the view.
 */

import { test, expect, type Page } from '@playwright/test';
import { deleteKVEntry } from './fixtures/security_seed.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

async function gotoKVStore(page: Page): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/kv'; });
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });
}

// ---------------------------------------------------------------------------
// Shared test state
// ---------------------------------------------------------------------------

// Entity type matching the KVStoreView default; tests the no-op path without
// changing the entity-type input.
const ENTITY_TYPE = 'default';

// Unique key to avoid collision between CI runs.
const KV_KEY = `e2e-kv-key-${Date.now()}`;

// Unique sentinel value — used in the detail modal assertion.
const KV_VALUE_SENTINEL = `e2e-kv-val-${Date.now()}`;
const KV_VALUE_JSON = JSON.stringify({ e2e: 'roundtrip', sentinel: KV_VALUE_SENTINEL });

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe.serial('Data > KV Store — put/list/detail/delete round-trip', () => {

  test.afterAll(async () => {
    // deleteKVEntry swallows 404 and transport errors.
    await deleteKVEntry(ENTITY_TYPE, KV_KEY);
  });

  // -------------------------------------------------------------------------
  // Test 1 — page renders
  // -------------------------------------------------------------------------
  test('1. KV Store page renders heading, controls, and Put Key button', async ({ page }) => {
    await gotoKVStore(page);

    await expect(page.getByRole('heading', { name: 'KV Store' })).toBeVisible();

    // Entity-type input (defaults to "default").
    const entityTypeInput = page.locator('input[placeholder="default"]');
    await expect(entityTypeInput).toBeVisible();

    // Filter-by-key input.
    const filterInput = page.locator('input[placeholder="Filter by key…"]');
    await expect(filterInput).toBeVisible();

    // Primary action button.  Two "Put Key" buttons exist when the store is
    // empty: the header toolbar button (line 195) and the EmptyState action
    // button (actionLabel="Put Key", line 248).  The header button is first in
    // DOM order (PageHeader renders before the {:else if} EmptyState block), so
    // .first() is safe and deterministic.
    await expect(
      page.getByRole('button').filter({ hasText: /Put Key/i }).first(),
    ).toBeVisible();
  });

  // -------------------------------------------------------------------------
  // Test 2 — Put Key dialog opens with correct form fields
  // -------------------------------------------------------------------------
  test('2. Put Key dialog opens with key input and value textarea', async ({ page }) => {
    await gotoKVStore(page);

    await page.getByRole('button').filter({ hasText: /Put Key/i }).first().click();

    // The dialog is a native <dialog> element (KVStoreView.svelte line 306).
    const dialog = page.getByRole('dialog', { name: /Put key-value dialog/i });
    await expect(dialog).toBeVisible();

    // Key input: input[type="text"] inside the dialog (no id attribute).
    await expect(dialog.locator('input[type="text"]')).toBeVisible();

    // Value textarea: textarea inside the dialog (no id attribute).
    await expect(dialog.locator('textarea')).toBeVisible();

    // Action buttons.
    await expect(dialog.getByRole('button', { name: /^Store$/ })).toBeVisible();
    await expect(dialog.getByRole('button', { name: /Cancel/i })).toBeVisible();

    // Close without submitting.
    await dialog.getByRole('button', { name: /Cancel/i }).click();
    await expect(dialog).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 3 — PUT /api/v1/kv/{type}/{key} returns 2xx, dialog closes
  // -------------------------------------------------------------------------
  test('3. PUT /api/v1/kv/default/{key} returns 2xx and dialog closes', async ({ page }) => {
    await gotoKVStore(page);

    // Arm response interceptor before opening the dialog.
    const putResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/kv/') &&
        (resp.request().method() === 'PUT' || resp.request().method() === 'POST'),
    );

    await page.getByRole('button').filter({ hasText: /Put Key/i }).first().click();

    const dialog = page.getByRole('dialog', { name: /Put key-value dialog/i });
    await expect(dialog).toBeVisible();

    // Fill the key field (input[type="text"] inside Put dialog).
    await dialog.locator('input[type="text"]').fill(KV_KEY);

    // Fill the JSON value textarea.
    await dialog.locator('textarea').fill(KV_VALUE_JSON);

    // Submit.
    await dialog.getByRole('button', { name: /^Store$/ }).click();

    const resp = await putResponse;
    expect(
      resp.status(),
      `PUT /api/v1/kv/${ENTITY_TYPE}/${KV_KEY} expected 2xx, got ${resp.status()}`,
    ).toBeGreaterThanOrEqual(200);
    expect(resp.status()).toBeLessThan(300);

    // Dialog must close after successful write.
    await expect(dialog).toHaveCount(0, { timeout: 5_000 });
  });

  // -------------------------------------------------------------------------
  // Test 4 — key appears in list with a preview
  // -------------------------------------------------------------------------
  test('4. stored key appears as a row in the KV list with a non-empty preview', async ({ page }) => {
    await gotoKVStore(page);

    // The table renders after fetchEntries() completes.
    // Wait for the table body (not the empty-state EmptyState component).
    const tableBody = page.locator('table.table-zebra tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    // The key cell is td.font-mono.text-xs (first column).
    const keyRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: KV_KEY }),
    });
    await expect(
      keyRow,
      `Expected KV entry row for key "${KV_KEY}" in the list`,
    ).toBeVisible({ timeout: 8_000 });

    // Preview cell (second column, td index 1) shows a truncated JSON string.
    // Using nth(1) rather than a class selector because the Tailwind class
    // "text-base-content/50" contains a "/" which requires CSS-escaping and
    // varies across browsers in querySelector implementations.
    // It must be non-empty — if blank, the value was not stored correctly.
    const previewCell = keyRow.locator('td').nth(1);
    await expect(previewCell).not.toBeEmpty();
  });

  // -------------------------------------------------------------------------
  // Test 5 — detail modal shows the full stored JSON value
  // -------------------------------------------------------------------------
  test('5. Eye button opens Key detail modal showing full JSON value', async ({ page }) => {
    await gotoKVStore(page);

    const tableBody = page.locator('table.table-zebra tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    const keyRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: KV_KEY }),
    });
    await expect(keyRow).toBeVisible();

    // Click the eye (View) icon button: aria-label="View {key}".
    await keyRow.getByRole('button', { name: new RegExp(`View ${KV_KEY}`) }).click();

    // Detail modal: <dialog aria-label="Key detail modal"> (line 395).
    const detailModal = page.getByRole('dialog', { name: /Key detail modal/i });
    await expect(detailModal).toBeVisible({ timeout: 5_000 });

    // The modal header shows the key name.
    await expect(detailModal.locator('p.font-mono')).toContainText(KV_KEY);

    // The <pre> block shows the full pretty-printed JSON value.
    // Assert the unique sentinel string from the stored value is present.
    await expect(detailModal.locator('pre')).toContainText(KV_VALUE_SENTINEL, {
      timeout: 3_000,
    });

    // Close the modal.
    await detailModal.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(detailModal).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 6 — Delete removes the key
  // -------------------------------------------------------------------------
  test('6. Delete icon + confirm issues DELETE /api/v1/kv/default/{key}; row removed', async ({ page }) => {
    await gotoKVStore(page);

    const tableBody = page.locator('table.table-zebra tbody');
    await expect(tableBody).toBeVisible({ timeout: 8_000 });

    const keyRow = tableBody.locator('tr').filter({
      has: page.locator('td.font-mono.text-xs', { hasText: KV_KEY }),
    });
    await expect(keyRow).toBeVisible();

    // Click the delete icon button: aria-label="Delete {key}".
    await keyRow.getByRole('button', { name: new RegExp(`Delete ${KV_KEY}`) }).click();

    // Confirm dialog: <dialog aria-label="Delete key confirmation"> (line 375).
    const confirmDialog = page.getByRole('dialog', { name: /Delete key confirmation/i });
    await expect(confirmDialog).toBeVisible();

    // The key name is shown in the confirm text.
    await expect(confirmDialog).toContainText(KV_KEY);

    // Arm DELETE response interceptor before confirming.
    const deleteResponse = page.waitForResponse(
      (resp) =>
        resp.url().includes('/api/v1/kv/') &&
        resp.request().method() === 'DELETE',
    );

    await confirmDialog.getByRole('button', { name: /^Delete$/ }).click();

    const delResp = await deleteResponse;
    expect(
      delResp.status(),
      `DELETE /api/v1/kv/${ENTITY_TYPE}/${KV_KEY} expected 2xx, got ${delResp.status()}`,
    ).toBeLessThan(300);

    // Row must disappear from the list.
    await expect(keyRow).toHaveCount(0, { timeout: 5_000 });
  });
});
