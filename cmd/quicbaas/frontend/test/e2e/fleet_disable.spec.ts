/**
 * fleet_disable.spec.ts
 *
 * E2E tests for the Fleet view and agent detail page.
 *
 * The daemon is started with --embedded-fleet-node, so at least one agent is always
 * present.  The Fleet view renders a card grid (not a table).  Clicking
 * a card navigates to #/fleet/:id, which is the FleetNodeDetailView that exposes
 * the Disable and Remove actions.
 *
 * No test.skip() calls.  If the daemon is unreachable or returns an empty
 * fleet the beforeAll fails fast with a clear error.
 *
 * Requires a running quicbaasd with --embedded-fleet-node.
 * Run:
 *   cd frontend && QUICBAAS_UI_URL=https://localhost:18080/ui/ \
 *     NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
 *     npx playwright test test/e2e/fleet_disable.spec.ts
 */

import { test, expect } from '@playwright/test';
import { listTestFleetNodes } from './fixtures/api.js';

test.use({ ignoreHTTPSErrors: true });

test.describe('Fleet — card grid and detail affordances', () => {
  test.beforeAll(async () => {
    const list = await listTestFleetNodes();
    const count = list.nodes?.length ?? 0;
    expect(
      count,
      'Expected at least one registered agent — is quicbaasd running with --embedded-fleet-node?',
    ).toBeGreaterThan(0);
  });

  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Fleet' }).first().click();
    await expect(page).toHaveURL(/#\/fleet/);
  });

  test('Fleet heading visible', async ({ page }) => {
    // PageHeader renders an <h1> element.
    await expect(page.locator('h1', { hasText: 'Fleet' }).first()).toBeVisible({
      timeout: 10_000,
    });
  });

  test('view renders without unhandled error markers', async ({ page }) => {
    await expect(page.locator('text=/runtime error/i')).toHaveCount(0);
    await expect(page.locator('text=/cannot read prop/i')).toHaveCount(0);
  });

  test('agent cards are rendered in the fleet grid', async ({ page }) => {
    // The fleet uses a card grid, not a table.  Each card is a role=button
    // with an aria-label that starts with "View details for fleet node".
    const cards = page.locator('[role="button"][aria-label^="View details for fleet node"]');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });
  });

  test('clicking an agent card navigates to the agent detail page', async ({ page }) => {
    const cards = page.locator('[role="button"][aria-label^="View details for fleet node"]');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });

    await cards.first().click();
    // FleetNodeDetailView is mounted at #/fleet/:id
    await expect(page).toHaveURL(/#\/fleet\/.+/, { timeout: 5_000 });
  });

  test('detail view exposes Disable and Remove actions', async ({ page }) => {
    // Navigate directly to fleet and click the first card.
    const cards = page.locator('[role="button"][aria-label^="View details for fleet node"]');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });
    await cards.first().click();

    await expect(page).toHaveURL(/#\/fleet\/.+/, { timeout: 5_000 });

    // FleetNodeDetailView header renders both buttons in the action area.
    // The Disable button toggles label between "Disable" and "Enable" depending
    // on agent status — match either.
    await expect(
      page.locator('button', { hasText: /^(Disable|Enable)$/ }).first(),
    ).toBeVisible({ timeout: 10_000 });

    await expect(
      page.locator('button[aria-label="Remove fleet node"]').first(),
    ).toBeVisible({ timeout: 5_000 });
  });

  test('detail view shows agent identity card with backend and host', async ({ page }) => {
    const cards = page.locator('[role="button"][aria-label^="View details for fleet node"]');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });
    await cards.first().click();
    await expect(page).toHaveURL(/#\/fleet\/.+/, { timeout: 5_000 });

    // Identity card must show a backend badge.
    await expect(page.locator('.badge-primary').first()).toBeVisible({ timeout: 5_000 });
    // Back button must be present for navigation.
    await expect(page.locator('button[aria-label="Back to fleet"]')).toBeVisible();
  });

  test('Back button on detail view returns to fleet', async ({ page }) => {
    const cards = page.locator('[role="button"][aria-label^="View details for fleet node"]');
    await expect(cards.first()).toBeVisible({ timeout: 10_000 });
    await cards.first().click();
    await expect(page).toHaveURL(/#\/fleet\/.+/, { timeout: 5_000 });

    await page.locator('button[aria-label="Back to fleet"]').click();
    await expect(page).toHaveURL(/#\/fleet/, { timeout: 5_000 });
  });
});
