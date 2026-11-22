/**
 * backup_view.spec.ts
 *
 * Cluster > Backup: the sidebar entry is enabled, #/backup lists the backups
 * GET /api/v1/backups returns, and the Create form POSTs the typed name.
 * The backups API is stubbed with page.route so the spec does not depend on
 * the daemon's backup store.
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

async function navigateTo(page: Page, hash: string): Promise<void> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

const BACKUPS = [
  { backup_id: 'b1', name: 'nightly', timestamp: 1700000000, size: 2048, status: 'completed', shards: [1, 2] },
  { backup_id: 'b2', name: 'weekly', timestamp: 1700100000, size: 4096, status: 'completed', shards: [1] },
];

test.describe('Cluster > Backup', () => {
  test('lists backups and creates one', async ({ page }) => {
    const posted: unknown[] = [];
    await page.route('**/api/v1/backups**', async (route) => {
      const req = route.request();
      if (req.method() === 'POST') {
        posted.push(req.postDataJSON());
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ backup_id: 'b3', timestamp: 1700200000, size: 0, status: 'pending' }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ backups: BACKUPS, total_count: BACKUPS.length }),
      });
    });

    await navigateTo(page, '#/backup');
    await expect(page.getByRole('heading', { name: 'Backup', level: 1 })).toBeVisible();

    const table = page.getByRole('table', { name: 'Backups' });
    await expect(table.locator('tbody tr')).toHaveCount(2);

    await page.getByLabel('Name', { exact: true }).fill('e2e-backup');
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect.poll(() => posted.length).toBe(1);
    expect(posted[0]).toMatchObject({ name: 'e2e-backup' });
  });

  test('the sidebar Backup entry is not marked soon', async ({ page }) => {
    await navigateTo(page, '#/dashboard');
    const entry = page.locator('button', { hasText: 'Backup' }).first();
    await expect(entry).toBeVisible();
    await expect(entry.locator('.badge', { hasText: 'soon' })).toHaveCount(0);
  });
});
