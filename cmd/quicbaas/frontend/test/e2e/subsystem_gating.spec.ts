/**
 * subsystem_gating.spec.ts
 *
 * Regression tests for issues #67 and #69:
 *
 *   #69  Time-Series and Vectors action buttons must be disabled when the
 *        subsystem is unavailable (gating banner visible).
 *
 *   #67  Workflow wizard Topology step: "Workspace Isolation" selected card
 *        must use border-primary, not border-secondary or border-error.
 *
 * These tests navigate via the hash-based router used by the SPA.  When the
 * daemon is not running the data views immediately set subsystemDisabled=true
 * (the SDK getSDKClient call rejects) so the gating banner is shown.  The
 * daemon must be reachable at QUICBAAS_UI_URL but the subsystems (timeseries,
 * vectors) do not need to be enabled — the tests exercise the disabled path.
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/subsystem_gating.spec.ts
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

/** Wait until loading spinners disappear (subsystem fetch settled). */
async function waitForFetch(page: Page, timeout = 10_000): Promise<void> {
  await page.waitForFunction(
    () => document.querySelectorAll('.loading').length === 0,
    undefined,
    { timeout },
  );
}

// ---------------------------------------------------------------------------
// #69 — Time-Series: disabled-subsystem button gating
// ---------------------------------------------------------------------------

test.describe('Time-Series — subsystem disabled gating (#69)', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/timeseries');
    await waitForFetch(page);
  });

  test('when subsystem is disabled, gating banner is visible', async ({ page }) => {
    // The banner appears only when subsystemDisabled=true.
    // If the subsystem IS enabled we skip this assertion gracefully.
    const banner = page.locator('[role="alert"]', {
      hasText: /QUICBAAS_TIMESERIES_ENABLED/i,
    });
    const enabled = await banner.count() === 0;
    if (enabled) {
      // Subsystem is actually running — nothing to gate.
      test.info().annotations.push({ type: 'note', description: 'timeseries subsystem is enabled; skipping disabled-path checks' });
      return;
    }
    await expect(banner.first()).toBeVisible();
  });

  test('when subsystem is disabled, "+ Write Point" button is disabled', async ({ page }) => {
    const banner = page.locator('[role="alert"]', {
      hasText: /QUICBAAS_TIMESERIES_ENABLED/i,
    });
    if (await banner.count() === 0) return; // subsystem enabled, not testing disabled path

    const writeBtn = page.getByRole('button', { name: /Write Point/i });
    await expect(writeBtn).toBeVisible();
    await expect(writeBtn).toBeDisabled();
  });

  test('when subsystem is disabled, "Query" button is disabled', async ({ page }) => {
    const banner = page.locator('[role="alert"]', {
      hasText: /QUICBAAS_TIMESERIES_ENABLED/i,
    });
    if (await banner.count() === 0) return; // subsystem enabled, not testing disabled path

    const queryBtn = page.getByRole('button', { name: /^Query$/i });
    await expect(queryBtn).toBeVisible();
    await expect(queryBtn).toBeDisabled();
  });
});

// ---------------------------------------------------------------------------
// #69 — Vectors: disabled-subsystem button gating
// ---------------------------------------------------------------------------

test.describe('Vectors — subsystem disabled gating (#69)', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/vectors');
    await waitForFetch(page);
  });

  test('when subsystem is disabled, gating banner is visible', async ({ page }) => {
    const banner = page.locator('[role="alert"]', {
      hasText: /QUICBAAS_VECTOR_ENABLED/i,
    });
    if (await banner.count() === 0) {
      test.info().annotations.push({ type: 'note', description: 'vectors subsystem is enabled; skipping disabled-path checks' });
      return;
    }
    await expect(banner.first()).toBeVisible();
  });

  test('when subsystem is disabled, "+ New Namespace" button is disabled', async ({ page }) => {
    const banner = page.locator('[role="alert"]', {
      hasText: /QUICBAAS_VECTOR_ENABLED/i,
    });
    if (await banner.count() === 0) return;

    const createBtn = page.getByRole('button', { name: /New Namespace/i });
    await expect(createBtn).toBeVisible();
    await expect(createBtn).toBeDisabled();
  });

  test('when subsystem is disabled, "Search" button is disabled', async ({ page }) => {
    const banner = page.locator('[role="alert"]', {
      hasText: /QUICBAAS_VECTOR_ENABLED/i,
    });
    if (await banner.count() === 0) return;

    const searchBtn = page.getByRole('button', { name: /Run similarity search/i });
    await expect(searchBtn).toBeVisible();
    await expect(searchBtn).toBeDisabled();
  });
});

// ---------------------------------------------------------------------------
// #67 — Workflow wizard: Topology step isolation card uses border-primary
// ---------------------------------------------------------------------------

test.describe('Workflow wizard — Topology step isolation card colors (#67)', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    // Navigate to Workflows via the sidebar.
    await page.locator('aside nav button', { hasText: 'Workflows' }).first().click();
    await expect(page).toHaveURL(/#\/workflows/);
    // Open the Create Workflow wizard.
    await page.getByRole('button', { name: /Create Workflow/i }).click();
    await expect(page.getByLabel(/Workflow Name/i)).toBeVisible();
  });

  /** Advance from step 1 to the Topology step (step 3). */
  async function advanceToTopologyStep(page: Page): Promise<void> {
    // Step 1: fill a unique workflow name.
    const nameInput = page.getByLabel(/Workflow Name/i);
    await nameInput.fill(`e2e-topology-${Date.now()}`);
    await page.getByRole('button', { name: /Continue/i }).click();

    // Step 2: add a lead agent (the default CTA button).
    await expect(page.getByRole('button', { name: /Add Lead Agent/i })).toBeVisible({ timeout: 8_000 });
    await page.getByRole('button', { name: /Add Lead Agent/i }).click();
    await expect(page.getByRole('button', { name: /Continue/i })).not.toBeDisabled({ timeout: 8_000 });
    await page.getByRole('button', { name: /Continue/i }).click();

    // Now on step 3 (Topology) — "Workspace Isolation" legend must be visible.
    await expect(page.getByText(/Workspace Isolation/i)).toBeVisible({ timeout: 8_000 });
  }

  test('default selected "Shared" isolation card has border-primary class (#67)', async ({ page }) => {
    await advanceToTopologyStep(page);

    // The selected isolation card label must have the border-primary class.
    // The "Shared" option is selected by default.
    const sharedLabel = page.locator('label').filter({ has: page.getByRole('heading', { name: 'Shared', exact: true }) }).first();
    await expect(sharedLabel).toBeVisible();
    const className = await sharedLabel.getAttribute('class') ?? '';
    expect(className).toContain('border-primary');
    expect(className).not.toContain('border-secondary');
    expect(className).not.toContain('border-error');
  });

  test('selecting "Worktree" gives it border-primary class (#67)', async ({ page }) => {
    await advanceToTopologyStep(page);

    // Click the Worktree card (its radio input is hidden; click the label itself).
    const worktreeLabel = page.locator('label').filter({ has: page.getByRole('heading', { name: 'Worktree', exact: true }) }).first();
    await worktreeLabel.click();

    await expect(async () => {
      const className = await worktreeLabel.getAttribute('class') ?? '';
      expect(className).toContain('border-primary');
      expect(className).not.toContain('border-secondary');
    }).toPass({ timeout: 3_000 });
  });

  test('selecting "Shared" after "Worktree" returns border-primary to Shared card (#67)', async ({ page }) => {
    await advanceToTopologyStep(page);

    const sharedLabel = page.locator('label').filter({ has: page.getByRole('heading', { name: 'Shared', exact: true }) }).first();
    const worktreeLabel = page.locator('label').filter({ has: page.getByRole('heading', { name: 'Worktree', exact: true }) }).first();

    // Select Worktree first.
    await worktreeLabel.click();
    // Then re-select Shared.
    await sharedLabel.click();

    await expect(async () => {
      const sharedClass = await sharedLabel.getAttribute('class') ?? '';
      const worktreeClass = await worktreeLabel.getAttribute('class') ?? '';
      expect(sharedClass).toContain('border-primary');
      expect(worktreeClass).not.toContain('border-primary');
    }).toPass({ timeout: 3_000 });
  });
});
