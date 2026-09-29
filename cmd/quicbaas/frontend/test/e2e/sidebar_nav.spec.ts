/**
 * sidebar_nav.spec.ts
 *
 * Regression tests for sidebar navigation.
 *
 * For each sidebar item we verify:
 *   1. Clicking the button updates page.url() to contain the expected hash.
 *   2. The button gains aria-current="page" (active marker).
 *   3. The breadcrumb text in the top header updates to the expected label.
 *
 * Sidebar structure after the dashboard split:
 *   OVERVIEW
 *     Dashboard          → #/dashboard   (system-wide)
 *   ORCHESTRATION
 *     Dashboard          → #/orchestration
 *     Jobs               → #/jobs
 *     Fleet              → #/fleet
 *     Sessions           → #/sessions
 *     Tasks              → #/tasks
 *     Live Events        → #/events
 *     Cost Tracking      → #/cost
 *
 * Sessions/Fleet/+Spawn NO LONGER appear in the top header.
 *
 * To run (requires a running quicbaasd at QUICBAAS_UI_URL):
 *   cd frontend && npx playwright test test/e2e/sidebar_nav.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Sidebar items: label as it appears in the DOM, expected hash, expected
// breadcrumb text (last breadcrumb <li>).
// ---------------------------------------------------------------------------

// Sidebar labels are now disambiguated by section name so each entry is
// unique within the sidebar (no more three "Dashboard" buttons). The
// breadcrumb still reads "Dashboard" as the leaf segment because the
// rendered surface in each case is a dashboard view; it's the parent
// breadcrumb segment (Overview / Software Factory / Cluster) that
// differentiates them.
const sidebarItems = [
  // OVERVIEW section
  { section: 'OVERVIEW',       label: 'Overview',      hash: '#/dashboard',     breadcrumb: 'Dashboard',      buttonIndex: 0 },
  // SOFTWARE FACTORY section
  { section: 'SOFTWARE FACTORY', label: 'Software Factory', hash: '#/orchestration', breadcrumb: 'Dashboard', buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Jobs',          hash: '#/jobs',          breadcrumb: 'Jobs',           buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Fleet',         hash: '#/fleet',         breadcrumb: 'Fleet',          buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Sessions',      hash: '#/sessions',      breadcrumb: 'Sessions',       buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Tasks',         hash: '#/tasks',         breadcrumb: 'Tasks',          buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Workflows',     hash: '#/workflows',     breadcrumb: 'Workflows',      buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Projects',      hash: '#/projects',      breadcrumb: 'Projects',       buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Live Events',   hash: '#/events',        breadcrumb: 'Live Events',    buttonIndex: 0 },
  { section: 'SOFTWARE FACTORY', label: 'Cost Tracking', hash: '#/cost',          breadcrumb: 'Cost Tracking',  buttonIndex: 0 },
] as const;

test.describe('Sidebar navigation — hash routing', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await expect(page.locator('body')).not.toBeEmpty();
  });

  for (const { label, hash, breadcrumb, buttonIndex } of sidebarItems) {
    test(`clicking "${label}" (${hash}) sets hash and updates breadcrumb`, async ({ page }) => {
      // Locate the sidebar button by visible text and disambiguate with nth()
      // for labels that appear more than once (e.g. "Dashboard" in OVERVIEW
      // and ORCHESTRATION sections).
      const btn = page.locator('aside nav button', { hasText: label }).nth(buttonIndex);
      await expect(btn).toBeVisible();

      await btn.click();

      // 1. URL contains the expected hash.
      await expect(page).toHaveURL(new RegExp(hash.replace('/', '\\/').replace('#', '#?')));

      // 2. The button is now the active item.
      await expect(btn).toHaveAttribute('aria-current', 'page');

      // 3. Breadcrumb last item reflects the active view.
      const breadcrumbEl = page.locator('.breadcrumbs li').last();
      await expect(breadcrumbEl).toContainText(breadcrumb);
    });
  }

  test('navigating back with the browser history button restores the previous hash', async ({ page }) => {
    const jobsBtn = page.locator('aside nav button', { hasText: 'Jobs' }).first();
    await jobsBtn.click();
    await expect(page).toHaveURL(/#\/jobs/);

    const sessionsBtn = page.locator('aside nav button', { hasText: 'Sessions' }).first();
    await sessionsBtn.click();
    await expect(page).toHaveURL(/#\/sessions/);

    await page.goBack();
    await expect(page).toHaveURL(/#\/jobs/);
    await expect(jobsBtn).toHaveAttribute('aria-current', 'page');
  });

  test('each implemented sidebar item has no "soon" badge', async ({ page }) => {
    // Only the items we have marked as implemented should lack the badge.
    const implementedLabels = ['Jobs', 'Fleet', 'Sessions', 'Tasks', 'Workflows', 'Projects', 'Live Events', 'Cost Tracking'];
    for (const label of implementedLabels) {
      const btn = page.locator('aside nav button', { hasText: label }).first();
      const soonBadge = btn.locator('.badge', { hasText: 'soon' });
      await expect(soonBadge).toHaveCount(0);
    }
  });

  test('top header does NOT contain Sessions or Fleet nav links', async ({ page }) => {
    // The primary navigation block was removed from the header.
    const header = page.locator('header');
    await expect(header.locator('[aria-label="Primary navigation"]')).toHaveCount(0);
    // No anchor link to #/sessions in the header.
    await expect(header.locator('a[href="#/sessions"]')).toHaveCount(0);
    await expect(header.locator('a[href="#/fleet"]')).toHaveCount(0);
  });

  /**
   * Regression: the bookmark-friendly hash #/live-events used to fall through
   * to the placeholder "feature is under development" view because the router
   * did not register it as an alias. #/fleet is the fleet's only route: the
   * retired #/agents alias is kept free for AI agent definitions.
   */
  test('direct-nav to #/fleet lands on the Fleet view', async ({ page }) => {
    await page.evaluate(() => { window.location.hash = '#/fleet'; });
    await page.waitForLoadState('networkidle');

    const breadcrumbEl = page.locator('.breadcrumbs li').last();
    await expect(breadcrumbEl).toContainText('Fleet');

    const btn = page.locator('aside nav button', { hasText: 'Fleet' }).first();
    await expect(btn).toHaveAttribute('aria-current', 'page');

    await expect(page.locator('main')).not.toContainText(/under development/i);
  });

  test('direct-nav to #/live-events lands on the Live Events view', async ({ page }) => {
    await page.evaluate(() => { window.location.hash = '#/live-events'; });
    await page.waitForLoadState('networkidle');

    const breadcrumbEl = page.locator('.breadcrumbs li').last();
    await expect(breadcrumbEl).toContainText('Live Events');

    const btn = page.locator('aside nav button', { hasText: 'Live Events' }).first();
    await expect(btn).toHaveAttribute('aria-current', 'page');

    await expect(page.locator('main')).not.toContainText(/under development/i);
  });
});
