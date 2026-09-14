/**
 * bug_regressions.spec.ts
 *
 * Regression tests for the multi-bug sweep:
 *
 *   A/B  IAM Tenants tab loads without freeze — page renders, no CDP timeout.
 *   C    IAM Users table renders username and created columns.
 *   D    Objects view shows ONE state at a time (error OR empty, not both).
 *   E    KMS view hides system-managed keys (algorithm="", created_at=zero-time).
 *   F    Cluster Dashboard renders in standalone mode.
 *
 * All tests run in the chromium-no-backend project (daemon at QUICBAAS_UI_URL).
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function navigateTo(page: Page, hash: string): Promise<void> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

async function waitForNoSpinner(page: Page, timeout = 10_000): Promise<void> {
  await page.waitForFunction(
    () => document.querySelectorAll('.loading').length === 0,
    undefined,
    { timeout },
  );
}

// ---------------------------------------------------------------------------
// Bug A/B — IAM Tenants tab — no renderer freeze
// ---------------------------------------------------------------------------

test.describe('IAM — no freeze on tab navigation', () => {
  test('Tenants tab renders without timeout or CDP freeze', async ({ page }) => {
    await navigateTo(page, '#/iam');

    // Switch to the Tenants tab — this was the freeze trigger.
    const tenantsTab = page.getByRole('tab', { name: /Tenants/i });
    await expect(tenantsTab).toBeVisible({ timeout: 8_000 });
    await tenantsTab.click();

    // If the page froze, the next assertion would time out.
    await waitForNoSpinner(page);

    // The heading must still be visible (page did not crash).
    await expect(page.getByRole('heading', { name: 'IAM' })).toBeVisible({ timeout: 5_000 });

    // Either a table row or "No tenants found" must appear — never a blank white screen.
    const rowOrEmpty = page.locator('tbody tr, .text-center');
    await expect(rowOrEmpty.first()).toBeVisible({ timeout: 8_000 });
  });

  test('IAM — KMS page loads without freeze', async ({ page }) => {
    await navigateTo(page, '#/kms');
    await waitForNoSpinner(page);
    await expect(page.getByRole('heading', { name: 'KMS' })).toBeVisible({ timeout: 8_000 });
  });
});

// ---------------------------------------------------------------------------
// Bug C — IAM Users table renders username and created columns
// ---------------------------------------------------------------------------

test.describe('IAM Users table — column bindings', () => {
  test('Users tab renders with username and created columns visible', async ({ page }) => {
    await navigateTo(page, '#/iam');
    await waitForNoSpinner(page);

    // Users tab is the default; wait for the table or empty state.
    const usersTab = page.getByRole('tab', { name: /Users/i });
    await expect(usersTab).toBeVisible({ timeout: 8_000 });

    // Wait for loading to complete.
    await waitForNoSpinner(page);

    // If there are rows, verify the USERNAME and CREATED columns are not empty.
    const rows = page.locator('tbody tr');
    const rowCount = await rows.count();

    if (rowCount > 0) {
      const firstRow = rows.first();
      // Username cell (1st column): must not be empty.
      const usernameCell = firstRow.locator('td').first();
      const usernameText = await usernameCell.textContent();
      expect((usernameText ?? '').trim().length).toBeGreaterThan(0);

      // Created cell (5th column — index 4): must not be empty.
      const createdCell = firstRow.locator('td').nth(4);
      const createdText = await createdCell.textContent();
      // Should be a date string or '—', NOT empty.
      expect(createdText?.trim()).not.toBe('');
    } else {
      // Empty state is also acceptable — no users in the tenant.
      const emptyOrTable = page.locator('.text-center, table');
      await expect(emptyOrTable.first()).toBeVisible({ timeout: 5_000 });
    }
  });

  test('Users tab does not show undefined or [object Object] values', async ({ page }) => {
    await navigateTo(page, '#/iam');
    await waitForNoSpinner(page);

    // When there are rows, check tbody for bad values; when empty, the tbody
    // may not exist — use textContent only if the element is present.
    const tbodyText = await page.locator('tbody').textContent().catch(() => '');
    expect(tbodyText).not.toContain('undefined');
    expect(tbodyText).not.toContain('[object Object]');
  });

  test('Users tab shows rows or empty-state — never a blank page', async ({ page }) => {
    await navigateTo(page, '#/iam');
    await waitForNoSpinner(page);

    // Either tbody rows OR an EmptyState message must be visible.
    // EmptyState does not render a tbody, so a locator targeting only tbody
    // would always fail on empty tenants. Use a combined assertion instead.
    const rowsOrEmpty = page
      .locator('tbody tr')
      .or(page.getByTestId('empty-state'))
      .or(page.getByText(/No users/i));

    await expect(rowsOrEmpty.first()).toBeVisible({ timeout: 8_000 });
  });
});

// ---------------------------------------------------------------------------
// Bug D — Objects view: one state at a time
// ---------------------------------------------------------------------------

test.describe('Objects view — mutually exclusive states', () => {
  test('Objects view shows EITHER error OR empty state, never both simultaneously', async ({ page }) => {
    await navigateTo(page, '#/objects');
    await waitForNoSpinner(page);

    const errorAlert = page.locator('[role="alert"]');
    const emptyState = page.locator('.text-center, [data-testid="empty-state"]');

    const hasError = await errorAlert.count() > 0;
    const hasEmpty = (await page.locator('text=No objects yet').count()) > 0;

    // They must not both be present at the same time.
    if (hasError && hasEmpty) {
      // If an error is shown, the empty state must not be visible.
      const errorVisible = await errorAlert.first().isVisible();
      const emptyVisible = await page.locator('text=No objects yet').first().isVisible();
      expect(errorVisible && emptyVisible).toBe(false);
    }

    // The page must not be completely empty (either state is expected).
    const pageContent = await page.locator('main, .p-6').first().textContent();
    expect((pageContent ?? '').trim().length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// Bug E — KMS view hides system-managed keys
// ---------------------------------------------------------------------------

test.describe('KMS view — system key filtering', () => {
  test('KMS view does not show keys with empty algorithm', async ({ page }) => {
    await navigateTo(page, '#/kms');
    await waitForNoSpinner(page);

    // Get all algorithm cells in the table.
    const algorithmCells = page.locator('tbody td:nth-child(2)');
    const count = await algorithmCells.count();

    for (let i = 0; i < count; i++) {
      const text = (await algorithmCells.nth(i).textContent() ?? '').trim();
      // Empty string algorithm means a system key — it must not appear.
      expect(text).not.toBe('');
    }
  });

  test('KMS view shows only tenant-managed keys (AES/RSA/ECDSA/ED25519)', async ({ page }) => {
    await navigateTo(page, '#/kms');
    await waitForNoSpinner(page);

    // If no tenant-managed keys exist, the empty state renders.
    // If keys exist, each must have a non-empty algorithm.
    const rows = page.locator('tbody tr');
    const rowCount = await rows.count();

    if (rowCount > 0) {
      for (let i = 0; i < rowCount; i++) {
        const algoCell = rows.nth(i).locator('td').nth(1);
        const algoText = (await algoCell.textContent() ?? '').trim();
        expect(algoText.length).toBeGreaterThan(0);
      }
    }
  });
});

// ---------------------------------------------------------------------------
// Bug F — Cluster Dashboard renders
// ---------------------------------------------------------------------------

test.describe('Cluster Dashboard', () => {
  test('Cluster > Dashboard renders without freezing', async ({ page }) => {
    await navigateTo(page, '#/cluster');
    await waitForNoSpinner(page);

    await expect(page.getByRole('heading', { name: 'Cluster' })).toBeVisible({ timeout: 8_000 });

    // Mode badge must appear: either "standalone" or "raft".
    const modeBadge = page.locator('.badge', { hasText: /standalone|raft/i });
    await expect(modeBadge.first()).toBeVisible({ timeout: 8_000 });
  });

  test('Cluster > Cluster sidebar item has no "soon" badge', async ({ page }) => {
    // The CLUSTER section's dashboard entry was renamed from "Dashboard"
    // to "Cluster" so the three section dashboards (Overview / Software Factory
    // / Cluster) are uniquely addressable in the sidebar.
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    const clusterBtn = page.locator('aside nav button', { hasText: 'Cluster' }).first();
    const soonBadge = clusterBtn.locator('.badge', { hasText: 'soon' });
    await expect(soonBadge).toHaveCount(0);
  });
});
