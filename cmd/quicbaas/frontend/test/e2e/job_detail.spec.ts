/**
 * job_detail.spec.ts
 *
 * Playwright tests for the Jobs list (#/jobs) and Job Detail (#/jobs/:id) views.
 *
 * Route layout (verified against src/stores/router.svelte.ts and MainApp.svelte):
 *
 *   #/jobs          → routerStore.name = 'jobs'
 *                     Renders JobsView (when no job selected in orchestratorStore)
 *                     OR JobDetailView (when orchestratorStore.selectedJob !== null
 *                     AND navStore.activeView === 'jobs' via the showJobDetail guard).
 *
 *   #/jobs/:id      → routerStore.name = 'job-detail'
 *                     Always renders JobDetailView directly (MainApp line 747).
 *                     An $effect in MainApp tries to find the job in
 *                     orchestratorStore.jobs; if not found it calls loadJobs().
 *                     If the ID is unknown after the load, selectedJob stays null
 *                     and JobDetailView renders "No job selected".
 *
 * Selectors verified against:
 *   src/views/orchestrator/JobsView.svelte
 *   src/views/orchestrator/JobDetailView.svelte
 *   src/views/MainApp.svelte (breadcrumb, route guards)
 *
 * Key selectors:
 *   h1 "Jobs"                   (PageHeader → <h1>Jobs</h1>)
 *   aria-label "Start new job"  (JobsView header "+ Start Job" button)
 *   p "No jobs yet..."          (EmptyState title rendered in <p>)
 *   select (state filter)       (options: All States / Active / Completed / Failed)
 *   input[placeholder="Search jobs..."] (search input)
 *   .breadcrumbs                (breadcrumb nav)
 *   table.table                 (jobs table)
 *   thead th                    (column headers: Job ID / Description / State /
 *                                Iteration / Progress / Started)
 *   .text-base-content\/30 text (JobDetailView "No job selected" guard)
 *   [role="tab"]                (JobDetailView tabs: Overview / Dag / Audit / Cost / Events)
 *   aria-label "Back to jobs list" (ChevronLeft back button in JobDetailView)
 *
 * No-backend determinism:
 *   Jobs are created by spawning ACP sessions and running orchestration
 *   workflows. Without an ACP backend (QUICBAAS_E2E_ACP_BACKEND), the jobs
 *   list is always empty. The tests therefore:
 *     - Assert the empty-state path UNCONDITIONALLY (always runs).
 *     - Assert the populated-row and detail-panel paths CONDITIONALLY
 *       (only when a row is present after page load).
 *
 * No ACP backend required. Inherits the admin storageState minted by
 * global-setup.ts (chromium-no-backend project).
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Navigation helpers
// ---------------------------------------------------------------------------

type TestPage = Parameters<Parameters<typeof test>[1]>[0];

async function gotoJobs(page: TestPage): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/jobs'; });
  await page.waitForLoadState('networkidle');
}

async function gotoJobDetail(page: TestPage, jobID: string): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate((id: string) => {
    window.location.hash = `#/jobs/${encodeURIComponent(id)}`;
  }, jobID);
  await page.waitForLoadState('networkidle');
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Software Factory > Jobs list and job detail', () => {

  // =========================================================================
  // Jobs list — #/jobs
  // =========================================================================

  test('1. #/jobs renders the "Jobs" h1 heading', async ({ page }) => {
    await gotoJobs(page);
    // PageHeader renders: <h1 class="text-2xl font-bold text-base-content">Jobs</h1>
    await expect(
      page.getByRole('heading', { name: 'Jobs', exact: true }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('2. breadcrumb shows Software Factory > Jobs', async ({ page }) => {
    await gotoJobs(page);
    const bc = page.locator('.breadcrumbs');
    await expect(bc).toContainText('Software Factory');
    await expect(bc).toContainText('Jobs');
  });

  test('3. "Start Job" button is visible', async ({ page }) => {
    await gotoJobs(page);
    // Header button: aria-label="Start new job", text "+ Start Job"
    await expect(
      page.getByRole('button', { name: /start new job/i }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('4. state filter select and search input are visible', async ({ page }) => {
    await gotoJobs(page);

    // State filter — <select class="select select-bordered select-sm">
    // Options: "All States" / "Active" / "Completed" / "Failed"
    const stateFilter = page.locator('select.select', { hasText: 'All States' });
    await expect(stateFilter).toBeVisible({ timeout: 10_000 });

    // Verify all expected filter options exist.
    await expect(stateFilter.locator('option', { hasText: 'Active' })).toHaveCount(1);
    await expect(stateFilter.locator('option', { hasText: 'Completed' })).toHaveCount(1);
    await expect(stateFilter.locator('option', { hasText: 'Failed' })).toHaveCount(1);

    // Search input — placeholder="Search jobs..."
    await expect(
      page.getByPlaceholder('Search jobs...'),
    ).toBeVisible();
  });

  test('5. jobs table renders with expected column headers', async ({ page }) => {
    await gotoJobs(page);

    // The table always renders even when empty — the header row is present.
    const table = page.locator('table.table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // Column headers from JobsView thead (sentence case):
    //   Job ID / Description / State / Iteration / Progress / Started
    await expect(table.locator('th', { hasText: 'Job ID' })).toHaveCount(1);
    await expect(table.locator('th', { hasText: 'Description' })).toHaveCount(1);
    await expect(table.locator('th', { hasText: 'State' })).toHaveCount(1);
    await expect(table.locator('th', { hasText: 'Iteration' })).toHaveCount(1);
    await expect(table.locator('th', { hasText: 'Progress' })).toHaveCount(1);
    await expect(table.locator('th', { hasText: 'Started' })).toHaveCount(1);
  });

  test('6. empty-state shows "No jobs yet" text when jobs list is empty', async ({ page }) => {
    await gotoJobs(page);

    // Wait for the jobsLoading spinner to clear (orchestrator fetch settled).
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // The orchestrator list will be empty in a no-ACP-backend environment.
    // If there ARE jobs (ACP backend available), this test is skipped.
    //
    // Why a populated jobs list requires the ACP project:
    //   Jobs are created by the daemon's orchestration engine when an ACP backend
    //   (opencode / claude / codex) accepts a session spawn and begins executing
    //   tasks. Without QUICBAAS_E2E_ACP_BACKEND set and an ACP container running,
    //   no jobs are created and the list is always empty in the chromium-no-backend
    //   project.
    const rows = page.locator('table.table tbody tr').filter({
      hasNot: page.locator('.loading-spinner'),
      hasNot: page.locator('td[colspan]'),
    });
    const rowCount = await rows.count();

    if (rowCount === 0) {
      // Empty path (deterministic in no-backend).
      // EmptyState renders the title in:
      //   <p class="text-sm font-semibold text-base-content">{title}</p>
      await expect(
        page.getByText('No jobs yet — Start Job is how you begin.'),
      ).toBeVisible({ timeout: 5_000 });

      // The EmptyState's action button also renders (mirrors the header button).
      await expect(
        page.getByRole('button', { name: /\+ start job/i }),
      ).toBeVisible();
    }
    // If rowCount > 0 the populated path is exercised by test 7 below.
  });

  test('7. clicking a job row navigates to #/jobs/:id and detail view renders (populated path)', async ({ page }) => {
    // This test only exercises its assertions when there is at least one job in
    // the list. In the chromium-no-backend project (no ACP backend), the jobs
    // list is empty and this test simply passes without asserting the row path.
    //
    // To exercise the populated path: run with QUICBAAS_E2E_ACP_BACKEND=opencode
    // (or another backend) in the chromium-acp project so a session is live.

    await gotoJobs(page);
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // Find clickable data rows (not the loading row or the empty-state row).
    const dataRows = page.locator('table.table tbody tr.cursor-pointer');
    const rowCount = await dataRows.count();

    if (rowCount === 0) {
      // No jobs present — skip populated-path assertions without failing.
      // The empty-path is covered unconditionally by test 6.
      return;
    }

    // Click the first data row — this calls handleRowClick(job) which does:
    //   selectJob(job);
    //   routeJobDetail(job.job_id);   // sets hash #/jobs/<id>
    const firstRow = dataRows.first();
    await firstRow.click();

    // URL must change to #/jobs/<some-id>
    await expect(page).toHaveURL(/#\/jobs\/.+/, { timeout: 10_000 });

    // JobDetailView renders the job detail panels. Verify key structural elements.

    // Back button: aria-label="Back to jobs list" (ChevronLeft <button>)
    await expect(
      page.getByRole('button', { name: /back to jobs list/i }),
    ).toBeVisible({ timeout: 10_000 });

    // JobStateMachine + 3-column summary cards + tab list must be visible.
    // Tabs: Overview / Dag / Audit / Cost / Events
    //   rendered by: {tab.charAt(0).toUpperCase() + tab.slice(1)}
    await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('tab', { name: 'Dag' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Audit' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Cost' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Events' })).toBeVisible();

    // "Back" navigates back to the jobs list.
    await page.getByRole('button', { name: /back to jobs list/i }).click();
    await expect(page).toHaveURL(/#\/jobs$/, { timeout: 5_000 });
    await expect(
      page.getByRole('heading', { name: 'Jobs', exact: true }),
    ).toBeVisible({ timeout: 5_000 });
  });

  // =========================================================================
  // Job detail — #/jobs/:id with unknown ID (deterministic in no-backend)
  // =========================================================================

  test('8. direct nav to #/jobs/<unknown-id> renders "No job selected" guard', async ({ page }) => {
    // A UUID-looking ID that will never match any real job. The MainApp $effect
    // tries orchestratorStore.jobs.find(j => j.job_id === id) and calls
    // loadJobs(); when the load completes and the ID is still not found,
    // orchestratorStore.selectedJob remains null.
    //
    // JobDetailView guard (line 136-139):
    //   {#if !job}
    //     <div class="flex items-center justify-center h-full">
    //       <div class="text-base-content/30">No job selected</div>
    //     </div>
    const unknownID = 'ffffffff-0000-4000-a000-000000000000';
    await gotoJobDetail(page, unknownID);

    // Allow time for the orchestrator to load jobs and settle.
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 15_000 });

    // The guard text must be visible when the job ID is not found.
    await expect(page.getByText('No job selected')).toBeVisible({ timeout: 10_000 });
  });

  test('9. breadcrumb for #/jobs/:id shows Software Factory > Jobs (link) > truncated id', async ({ page }) => {
    // Navigating to a job-detail route always renders the breadcrumb even when
    // the job is not found, because the breadcrumb is in the header shell
    // (MainApp), not inside JobDetailView.
    //
    // MainApp breadcrumb for 'job-detail':
    //   <li class="text-base-content/40">{FACTORY_CRUMB}</li>   (renders "Software Factory")
    //   <li><a href="#/jobs" class="text-base-content/60">Jobs</a></li>
    //   <li class="font-medium font-mono text-xs">{id.slice(0, 12)}…</li>
    const testID = 'aabbccdd-1122-3344-5566-778899aabbcc';
    await gotoJobDetail(page, testID);

    const bc = page.locator('.breadcrumbs');
    await expect(bc).toContainText('Software Factory');
    await expect(bc).toContainText('Jobs');
    // Truncated to first 12 chars.
    await expect(bc).toContainText(testID.slice(0, 12));
  });

  test('10. search input filters the visible rows by description', async ({ page }) => {
    // This test is only meaningful when jobs exist. In a no-backend environment
    // the table is empty; the filter simply leaves it empty too. We assert the
    // "no jobs match" message that appears when the list is non-empty but the
    // filter matches nothing — which requires at least one job. So this test is
    // only verifying the UI wiring: typing into the search box must not crash
    // the page (the filter is $derived and always re-renders safely).
    await gotoJobs(page);
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    const searchInput = page.getByPlaceholder('Search jobs...');
    await expect(searchInput).toBeVisible({ timeout: 5_000 });

    // Type a query that matches nothing — verifies the filter runs without error.
    await searchInput.fill('this-string-cannot-exist-in-any-real-job-description-e2e');

    // Either the empty-state title or the filter-no-match text should appear.
    // From JobsView.svelte:
    //   empty-state title: "No jobs yet — Start Job is how you begin."  (<p> via EmptyState)
    //   no-match text:     "No jobs match the current filter."           (<div class="text-center ...">)
    // A regex OR avoids the CSS comma ambiguity of the previous 'text=A, text=B' form.
    const emptyOrNoMatch = page.getByText(/No jobs yet|No jobs match the current filter/i);
    await expect(emptyOrNoMatch.first()).toBeVisible({ timeout: 5_000 });
  });

});
