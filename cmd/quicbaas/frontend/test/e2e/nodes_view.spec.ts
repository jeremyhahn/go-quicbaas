/**
 * nodes_view.spec.ts
 *
 * Cluster > Nodes lists the cluster's members from GET /api/v1/cluster/nodes.
 * The API call is stubbed with the daemon's real response shape: every member
 * is reported healthy (from Raft membership, not a liveness check), with no
 * version or uptime, so the view must show those as not reported. The first
 * test opens #/nodes directly; the second reaches the view through the
 * sidebar's Nodes button.
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

const NODES = {
  nodes: [
    {
      node_id: 1,
      address: '10.0.0.1:7000',
      raft_address: '10.0.0.1:7000',
      is_leader: true,
      health_status: 'healthy',
      version: '',
      uptime_seconds: 0,
    },
    {
      node_id: 2,
      address: '10.0.0.2:7000',
      raft_address: '10.0.0.2:7000',
      is_leader: false,
      health_status: 'healthy',
      version: '',
      uptime_seconds: 0,
    },
  ],
  total_count: 2,
  timestamp: '2026-11-21T00:00:00Z',
};

async function navigateTo(page: Page, hash: string): Promise<void> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

async function stubNodes(page: Page): Promise<void> {
  await page.route('**/api/v1/cluster/nodes**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(NODES),
    });
  });
}

test.describe('Cluster > Nodes', () => {
  test('lists the stubbed nodes with the leader marked', async ({ page }) => {
    await stubNodes(page);

    await navigateTo(page, '#/nodes');

    await expect(page.getByRole('heading', { name: 'Nodes' })).toBeVisible({ timeout: 8_000 });
    const table = page.getByRole('table', { name: 'Cluster nodes' });
    await expect(table.locator('tbody tr')).toHaveCount(2, { timeout: 8_000 });
    await expect(table.locator('tbody td', { hasText: 'Shard leader' })).toHaveCount(1);
    await expect(table.locator('tbody td', { hasText: 'not reported' })).toHaveCount(4);
    await expect(table.getByRole('columnheader', { name: 'Health' })).toHaveCount(0);
  });

  test('the Nodes sidebar item opens the Nodes view', async ({ page }) => {
    await stubNodes(page);
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    const nodesBtn = page.locator('aside nav').getByRole('button', { name: 'Nodes', exact: true });
    await expect(nodesBtn).toBeVisible({ timeout: 8_000 });
    await expect(nodesBtn.locator('.badge', { hasText: 'soon' })).toHaveCount(0);
    await nodesBtn.click();
    await expect(page).toHaveURL(/#\/nodes/);
    await expect(page.getByRole('heading', { name: 'Nodes' })).toBeVisible({ timeout: 8_000 });
    const table = page.getByRole('table', { name: 'Cluster nodes' });
    await expect(table.locator('tbody tr')).toHaveCount(2, { timeout: 8_000 });
    await expect(table.locator('tbody td', { hasText: '10.0.0.2:7000' }).first()).toBeVisible();
  });
});
